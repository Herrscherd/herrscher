package supervisor

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// Supervisor manages one child `herrscher bridge` process per session.
type Supervisor struct {
	ctx     context.Context
	selfBin string // path to the herrscher binary (os.Executable)
	mu      sync.Mutex
	runs    map[string]*supervisedRun
	log     *slog.Logger
	// sleep and now are clock seams (default time.After / time.Now) so tests can
	// drive the restart loop and its backoff without real wall-clock waits.
	sleep func(time.Duration) <-chan time.Time
	now   func() time.Time
	// run is the supervised-loop seam. Production points it at runLoop; tests
	// can hold teardown at a deterministic boundary without spawning a process.
	run func(context.Context, state.Session)
	// metrics records bridge-restart counts (nil = no recording, e.g. in tests).
	metrics *metrics.Registry
	// agentsRoot is the directory holding agent homes, passed to each bridge so its
	// delegation roster matches the store the coordinator spawns from (which honours
	// the daemon's --state override). Empty = the bridge falls back to its default.
	agentsRoot string
	// bridgeEnv is extra KEY=VALUE entries added to each bridge child's
	// environment, on top of os.Environ(). The daemon strips the gateway
	// credentials from its own environment at startup so they cannot leak into a
	// vendor CLI; the bridge — the same trusted binary, and the process that
	// actually builds backends — gets them back this way. Deliberately NOT argv:
	// /proc/<pid>/cmdline is world readable.
	bridgeEnv []string
	// runBridge is the process boundary. Tests replace it with a controlled
	// runner so restart ordering can be asserted without launching a real agent.
	runBridge func(context.Context, state.Session)
}

type supervisedRun struct {
	cancel   context.CancelFunc
	done     chan struct{}
	stopping bool
}

// bridgeArgs builds the child `herrscher bridge` argv for sess.
func (s *Supervisor) bridgeArgs(sess state.Session) []string {
	args := []string{"bridge", "-c", sess.ChannelID, "--cmd", sess.Cmd, "--session", sess.Name,
		"--hub-socket", control.SocketPath(sess.Name)}
	// P1: thread the session's memory scope so the orchestrator recalls the
	// game's shared memory and this agent's private skills each turn. The bridge
	// flags are memory-only by definition, so a memory root wins over the
	// placement field of the same name, for both halves of the scope: Project may
	// be steering the session into a workspace sub-directory and Agent may be
	// demanding a worktree, and neither says where knowledge goes. No session
	// created before the memory roots existed sets either, so the precedence only
	// ever decides between two things one caller asked for at once.
	if p := sess.MemoryProject; p != "" {
		args = append(args, "--project", p)
	} else if sess.Project != "" {
		args = append(args, "--project", sess.Project)
	}
	if a := sess.MemoryAgent; a != "" {
		args = append(args, "--agent", a)
	} else if sess.Agent != "" {
		args = append(args, "--agent", sess.Agent)
	}
	// Whether the bridge may revise the project on this session's first prompt.
	if sess.ProjectPinned {
		args = append(args, "--project-pinned")
	}
	if sess.Backend != "" && sess.Backend != "stream" {
		args = append(args, "--backend", sess.Backend)
	}
	if sess.Vendor != "" {
		args = append(args, "--vendor", sess.Vendor)
	}
	// The catalog model id is what makes the bridge's spawn go through the
	// routing choke point (host.BuildBackendFor). Without it a gateway-routed
	// session would spawn bare, i.e. on the machine's own vendor login — the
	// exact silence the routing feature exists to prevent.
	if sess.ModelID != "" {
		args = append(args, "--model", sess.ModelID)
	}
	// P1 write side (opt-in): thread the learning config so the bridge builds a
	// Learner instead of the plain Curator. Only when set, like the scope above.
	if sess.Extractor != "" {
		args = append(args, "--extractor", sess.Extractor)
	}
	if sess.Journal != "" {
		args = append(args, "--journal", sess.Journal)
	}
	if sess.ConsolidateEvery > 0 {
		args = append(args, "--consolidate-every", strconv.Itoa(sess.ConsolidateEvery))
	}
	if sess.ResumeToken != "" {
		args = append(args, "--resume", sess.ResumeToken)
	}
	if s.agentsRoot != "" {
		args = append(args, "--agents-root", s.agentsRoot)
	}
	return args
}

// NewSupervisor builds a Supervisor bound to ctx. It logs through a quiet
// default until SetLogger installs the daemon's operator logger.
func NewSupervisor(ctx context.Context, selfBin string) *Supervisor {
	s := &Supervisor{
		ctx:     ctx,
		selfBin: selfBin,
		runs:    map[string]*supervisedRun{},
		log:     obs.NewLogger(os.Stderr, slog.LevelInfo),
		sleep:   time.After,
		now:     time.Now,
	}
	s.runBridge = s.runBridgeCommand
	s.run = s.runLoop
	return s
}

// SetLogger installs the operator logger the supervisor logs restart events
// through (component=supervisor is attached for filtering).
func (s *Supervisor) SetLogger(l *slog.Logger) {
	s.log = l.With("component", "supervisor")
}

// SetMetrics installs the registry the supervisor records bridge restarts into.
func (s *Supervisor) SetMetrics(m *metrics.Registry) {
	s.metrics = m
}

// SetAgentsRoot records the agent-home directory threaded to each spawned bridge
// as --agents-root, so a bridge's delegation roster is read from the same store
// the daemon manages even under a non-default --state path.
func (s *Supervisor) SetAgentsRoot(root string) {
	s.agentsRoot = root
}

// SetBridgeEnv records extra KEY=VALUE entries handed to every bridge child on
// top of the inherited environment. The composition root passes the captured
// gateway credentials here (see host.GatewayEnvPairs), which the daemon removed
// from its own environment so no vendor CLI can read them, and the summary of
// the verbs it dispatches (host.CapabilityEnvPair), which only the daemon holds.
func (s *Supervisor) SetBridgeEnv(kv []string) {
	s.bridgeEnv = append([]string(nil), kv...)
}

// Start launches a supervised bridge for sess (idempotent per name).
//
// It cannot fail, and says so: launching is handing the run loop a goroutine,
// and every way the bridge itself can fail happens later, inside that loop,
// where the supervisor's job is to retry rather than to report. An error return
// here would only be a nil every caller has to pretend to handle — which is what
// it was, at six call sites, two of them with an unreachable error branch.
func (s *Supervisor) Start(sess state.Session) {
	for {
		s.mu.Lock()
		run, running := s.runs[sess.Name]
		switch {
		case !running:
			cctx, cancel := context.WithCancel(s.ctx)
			run = &supervisedRun{cancel: cancel, done: make(chan struct{})}
			s.runs[sess.Name] = run
			s.mu.Unlock()
			go func() {
				defer close(run.done)
				s.run(cctx, sess)
			}()
			return
		case !run.stopping:
			s.mu.Unlock()
			return
		default:
			s.mu.Unlock()
			<-run.done
			s.mu.Lock()
			if s.runs[sess.Name] == run {
				delete(s.runs, sess.Name)
			}
			s.mu.Unlock()
		}
	}
}

// Stop terminates the bridge for name and waits until its supervised loop has
// returned. Callers may safely remove the bridge's working directory after Stop.
func (s *Supervisor) Stop(name string) error {
	s.mu.Lock()
	run, ok := s.runs[name]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	run.stopping = true
	run.cancel()
	s.mu.Unlock()

	<-run.done

	s.mu.Lock()
	if s.runs[name] == run {
		delete(s.runs, name)
	}
	s.mu.Unlock()
	return nil
}

// Running reports whether a supervised loop is currently held for name. It is
// the one question an operator (or a test) can ask about a bridge that is not
// answering: whether the daemon still believes it is running one.
func (s *Supervisor) Running(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[name]
	return ok && !run.stopping
}

// Restart synchronously replaces only sess.Name. The old process loop is fully
// stopped before the replacement is launched with sess's current Cmd/Vendor.
func (s *Supervisor) Restart(sess state.Session) error {
	if err := s.Stop(sess.Name); err != nil {
		return err
	}
	s.Start(sess)
	return nil
}

func (s *Supervisor) runLoop(ctx context.Context, sess state.Session) {
	bo := obs.RestartBackoff()
	for {
		if ctx.Err() != nil {
			return
		}
		start := s.now()
		s.runBridge(ctx, sess)
		if ctx.Err() != nil {
			return
		}
		s.metrics.BridgeRestart()
		delay := bo.Next(s.now().Sub(start))
		s.log.Warn("bridge exited, restarting", "session", sess.Name, "delay", delay)
		select {
		case <-ctx.Done():
			return
		case <-s.sleep(delay):
		}
	}
}

func (s *Supervisor) runBridgeCommand(ctx context.Context, sess state.Session) {
	cmd := s.bridgeCommand(ctx, sess)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	_ = cmd.Run() // returns on exit or ctx cancel
}

// bridgeCommand builds the child process for sess: argv, working directory and
// environment. Split out of runBridgeCommand so a test can assert what the
// child would receive — in particular that the gateway credentials ride the
// environment and never argv.
func (s *Supervisor) bridgeCommand(ctx context.Context, sess state.Session) *exec.Cmd {
	cmd := exec.CommandContext(ctx, s.selfBin, s.bridgeArgs(sess)...)
	configureBridgeCommand(cmd)
	// Dir is the resolved run directory (worktree, or workspace/project root);
	// fall back to Worktree for sessions persisted before Dir existed. Empty
	// leaves cmd.Dir unset so the child inherits the launcher's cwd.
	if dir := sess.Dir; dir != "" {
		cmd.Dir = dir
	} else if sess.Worktree != "" {
		cmd.Dir = sess.Worktree
	}
	cmd.Env = append(os.Environ(), s.bridgeEnv...)
	return cmd
}
