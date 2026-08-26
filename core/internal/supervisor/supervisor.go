package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/runner"
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
	// hosts answers where a session's process must run. A function rather than
	// the state itself, so this package stays testable and so `local` (the empty
	// host) needs no record to exist. nil = everything runs here.
	hosts func(name string) (Placement, bool)
	// instanceID and the command socket are what a remote bridge's children
	// resolve `herrscher <verb>` through. They are injected because the package
	// that knows those paths imports this one.
	instanceID     string
	cmdSocketLocal string
}

// Placement is what the supervisor needs to know about a host to launch there.
type Placement struct {
	SSH string // user@machine
	Bin string // absolute path to herrscher over there
}

// SetHostLookup installs the resolver from a session's host name to a placement.
func (s *Supervisor) SetHostLookup(f func(name string) (Placement, bool)) { s.hosts = f }

// SetInstanceID records the daemon's instance, threaded to a remote bridge so
// its children resolve the same command socket this daemon listens on.
func (s *Supervisor) SetInstanceID(id string) { s.instanceID = id }

// SetCommandSocket records the operator command socket this daemon listens on.
// Forwarding it is what keeps the <capabilities> block honest over there: it
// tells the agent it can run `herrscher <verb>`, and on another machine that is
// only true if the socket followed. A capabilities block that lies is a bug, not
// a limitation. Where it lands over there is per session, so the launch derives
// it rather than taking it here.
func (s *Supervisor) SetCommandSocket(local string) { s.cmdSocketLocal = local }

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
	cmd, err := s.bridgeCommand(ctx, sess)
	if err != nil {
		// Warn and wait, rather than retry: the cause is a configuration an
		// operator has to change, and a backoff loop would only bury the reason
		// under restarts. The loop resumes when the session is stopped or
		// restarted, which is exactly the moment the answer may have changed.
		s.log.Error("cannot launch bridge", "session", sess.Name, "host", sess.Host, "err", err)
		<-ctx.Done()
		return
	}
	if prep := s.prepareRemote(ctx, sess); prep != nil {
		// A stale socket from a crash blocks the bind on any sshd without
		// StreamLocalBindUnlink. The error is not read: if the removal failed for
		// a reason that matters, ExitOnForwardFailure says so next, in the
		// message that names the actual forward.
		_ = prep.Run()
	}
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	_ = cmd.Run() // returns on exit or ctx cancel
}

// bridgeCommand builds the child process for sess: argv, working directory,
// environment, and where it lands. Split out of runBridgeCommand so a test can
// assert what the child would receive, in particular that the gateway
// credentials ride the environment locally and stdin remotely, never argv.
func (s *Supervisor) bridgeCommand(ctx context.Context, sess state.Session) (*exec.Cmd, error) {
	if sess.Host != "" {
		return s.remoteBridgeCommand(ctx, sess)
	}
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
	cmd.Env = append(cmd.Env, control.SessionVar+"="+sess.Name)
	return cmd, nil
}

// remoteBridgeCommand builds the ssh invocation that runs the bridge on another
// machine. Nothing about the bridge changes: it dials a unix socket exactly as
// it does here, because -R put one at that path over there.
func (s *Supervisor) remoteBridgeCommand(ctx context.Context, sess state.Session) (*exec.Cmd, error) {
	p, err := s.placementFor(sess)
	if err != nil {
		return nil, err
	}
	args := s.bridgeArgs(sess)
	// The hub socket is the FORWARDED path, not this machine's: the bridge over
	// there dials what -R exposed.
	for i := range args {
		if args[i] == "--hub-socket" && i+1 < len(args) {
			args[i+1] = control.RemoteSocketPath(sess.Name)
		}
	}
	args = append(args, "--env-stdin")
	// No multiplexing here, unlike every short command this daemon runs over
	// ssh. A launch carries forwards, and a shared master owns the ones it
	// opened: a relaunch after a crash, or a second session on the same host,
	// would ask a master that already holds that path and be answered with
	// silence rather than a socket. Its own connection makes the forwards live
	// and die with the bridge, which is exactly their scope.
	r := runner.SSH{Target: p.SSH, Forwards: s.forwardsFor(sess)}
	dir := sess.Dir
	if dir == "" {
		dir = sess.Worktree
	}
	cmd := r.Command(ctx, dir, append([]string{p.Bin}, args...)...)
	cmd.Stdin = strings.NewReader(s.remoteEnvBlock(sess))
	return cmd, nil
}

// placementFor resolves sess.Host, and refuses when it cannot. There is
// deliberately no fallback to local: running here an agent the operator
// believed was elsewhere is exactly the class of silence routing exists to
// prevent.
func (s *Supervisor) placementFor(sess state.Session) (Placement, error) {
	if s.hosts == nil {
		return Placement{}, fmt.Errorf("session %q wants host %q but no host is registered", sess.Name, sess.Host)
	}
	p, ok := s.hosts(sess.Host)
	if !ok {
		return Placement{}, fmt.Errorf("session %q wants host %q, which no longer exists", sess.Name, sess.Host)
	}
	if p.SSH == "" || p.Bin == "" {
		return Placement{}, fmt.Errorf("host %q is not provisioned: run `host provision %s`", sess.Host, sess.Host)
	}
	return p, nil
}

// forwardsFor lists the sockets a remote bridge needs to reach back here: the
// session's control socket, and the daemon's operator command socket.
func (s *Supervisor) forwardsFor(sess state.Session) []runner.Forward {
	fwd := []runner.Forward{{
		Remote: control.RemoteSocketPath(sess.Name),
		Local:  control.SocketPath(sess.Name),
	}}
	if s.cmdSocketLocal != "" {
		fwd = append(fwd, runner.Forward{Remote: control.RemoteCommandSocketPath(sess.Name), Local: s.cmdSocketLocal})
	}
	return fwd
}

// prepareRemote returns the command that clears the far end of this session's
// forwards, or nil for a local session.
func (s *Supervisor) prepareRemote(ctx context.Context, sess state.Session) *exec.Cmd {
	if sess.Host == "" {
		return nil
	}
	p, err := s.placementFor(sess)
	if err != nil {
		return nil
	}
	r := runner.SSH{Target: p.SSH, ControlPath: runner.ControlPathFor(p.SSH), Forwards: s.forwardsFor(sess)}
	return r.PrepareForwards(ctx)
}

// remoteEnvBlock renders the environment a remote bridge reads from its stdin,
// terminated by the blank line that ends it.
//
// Three entries are added that a local bridge inherits from the daemon's own
// environment and a remote one cannot: the instance id, a TMPDIR matching the
// directory the forwards were bound at, and the command socket itself. That
// last one is not computable over there, since the path is per session; without
// it `herrscher <verb>` would dial a path nothing listens on, and the
// <capabilities> block would be a promise the agent cannot keep.
//
// The session name rides along too, the same one the local launch exports, so
// the approval hook over there can name the session it is asking for.
func (s *Supervisor) remoteEnvBlock(sess state.Session) string {
	env := map[string]string{}
	for _, kv := range s.bridgeEnv {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			env[k] = v
		}
	}
	if s.instanceID != "" {
		env["HERRSCHER_INSTANCE_ID"] = s.instanceID
	}
	env["TMPDIR"] = "/tmp"
	env[control.SessionVar] = sess.Name
	if s.cmdSocketLocal != "" {
		env[control.CommandSocketVar] = control.RemoteCommandSocketPath(sess.Name)
	}
	return contracts.EncodeEnvSetting(env) + "\n"
}
