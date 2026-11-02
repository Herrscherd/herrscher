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
	ctx              context.Context
	selfBin          string
	mu               sync.Mutex
	runs             map[string]*supervisedRun
	log              *slog.Logger
	sleep            func(time.Duration) <-chan time.Time
	now              func() time.Time
	run              func(context.Context, state.Session)
	metrics          *metrics.Registry
	agentsRoot       string
	bridgeEnv        []string
	runBridge        func(context.Context, state.Session)
	hosts            func(name string) (Placement, bool)
	instanceID       string
	sessionCmdSocket func(session string) string
	gate             func(vendor string) string
}

// Placement is what the supervisor needs to know about a host to launch there.
type Placement struct {
	SSH string
	Bin string
}

// SetHostLookup installs the resolver from a session's host name to a placement.
func (s *Supervisor) SetHostLookup(f func(name string) (Placement, bool)) { s.hosts = f }

// SetGateResolver wires the per-vendor approval granularity. It is the same
// answer the manager gets when it decides whether to warn, asked again here
// because only a backend that gates in-process has any use for the mode.
func (s *Supervisor) SetGateResolver(f func(vendor string) string) { s.gate = f }

// SetInstanceID records the daemon's instance, threaded to a remote bridge so
// its children resolve the same command socket this daemon listens on.
func (s *Supervisor) SetInstanceID(id string) { s.instanceID = id }

// SetCommandSocket names, per session, the command socket that session's
// processes dial. Forwarding it is what keeps the <capabilities> block honest
// over there: it tells the agent it can run `herrscher <verb>`, and on another
// machine that is only true if the socket followed. A capabilities block that
// lies is a bug, not a limitation.
//
// It is per session rather than one path for the daemon, because which listener
// a connection arrives on is what tells the daemon who is calling. A session
// pointed at the operator's socket would hold the operator's authority.
//
// A local bridge is given the very path this daemon bound, since a session's
// children must reach the daemon that started them and not the one the default
// state file happens to name.
func (s *Supervisor) SetCommandSocket(resolve func(session string) string) {
	s.sessionCmdSocket = resolve
}

type supervisedRun struct {
	cancel   context.CancelFunc
	done     chan struct{}
	stopping bool
}

func (s *Supervisor) bridgeArgs(sess state.Session) []string {
	args := []string{
		"bridge", "-c", sess.ChannelID, "--cmd", sess.Cmd, "--session", sess.Name,
		"--hub-socket", control.SocketPath(sess.Name),
	}
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
	if sess.ProjectPinned {
		args = append(args, "--project-pinned")
	}
	if sess.Backend != "" && sess.Backend != "stream" {
		args = append(args, "--backend", sess.Backend)
	}
	if sess.Vendor != "" {
		args = append(args, "--vendor", sess.Vendor)
	}
	if sess.ModelID != "" {
		args = append(args, "--model", sess.ModelID)
	}
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
// here would only be a nil every caller has to pretend to handle, which is what
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
		s.log.Error("cannot launch bridge", "session", sess.Name, "host", sess.Host, "err", err)
		<-ctx.Done()
		return
	}
	if prep := s.prepareRemote(ctx, sess); prep != nil {
		_ = prep.Run()
	}
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	_ = cmd.Run()
}

func (s *Supervisor) bridgeCommand(ctx context.Context, sess state.Session) (*exec.Cmd, error) {
	if sess.Host != "" {
		return s.remoteBridgeCommand(ctx, sess)
	}
	cmd := exec.CommandContext(ctx, s.selfBin, s.bridgeArgs(sess)...)
	configureBridgeCommand(cmd)
	if dir := sess.Dir; dir != "" {
		cmd.Dir = dir
	} else if sess.Worktree != "" {
		cmd.Dir = sess.Worktree
	}
	cmd.Env = append(os.Environ(), s.bridgeEnv...)
	cmd.Env = append(cmd.Env, control.SessionVar+"="+sess.Name)
	if s.sessionCmdSocket != nil {
		cmd.Env = append(cmd.Env, control.CommandSocketVar+"="+s.sessionCmdSocket(sess.Name))
	}
	cmd.Env = contracts.MergeEnv(cmd.Env, s.approvalsEnv(sess, s.selfBin))
	return cmd, nil
}

func (s *Supervisor) approvalsEnv(sess state.Session, bin string) map[string]string {
	if s.gate == nil || s.gate(sess.Vendor) != string(contracts.GrainTool) {
		return nil
	}
	return contracts.ApprovalsEnv(sess.Name, sess.Approvals, bin)
}

func (s *Supervisor) remoteBridgeCommand(ctx context.Context, sess state.Session) (*exec.Cmd, error) {
	p, err := s.placementFor(sess)
	if err != nil {
		return nil, err
	}
	args := s.bridgeArgs(sess)
	for i := range args {
		if args[i] == "--hub-socket" && i+1 < len(args) {
			args[i+1] = control.RemoteSocketPath(sess.Name)
		}
	}
	args = append(args, "--env-stdin")
	r := runner.SSH{Target: p.SSH, Forwards: s.forwardsFor(sess)}
	dir := sess.Dir
	if dir == "" {
		dir = sess.Worktree
	}
	cmd := r.Command(ctx, dir, append([]string{p.Bin}, args...)...)
	cmd.Stdin = strings.NewReader(s.remoteEnvBlock(sess))
	return cmd, nil
}

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

func (s *Supervisor) forwardsFor(sess state.Session) []runner.Forward {
	fwd := []runner.Forward{{
		Remote: control.RemoteSocketPath(sess.Name),
		Local:  control.SocketPath(sess.Name),
	}}
	if s.sessionCmdSocket != nil {
		fwd = append(fwd, runner.Forward{
			Remote: control.RemoteCommandSocketPath(sess.Name),
			Local:  s.sessionCmdSocket(sess.Name),
		})
	}
	return fwd
}

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
	if s.sessionCmdSocket != nil {
		env[control.CommandSocketVar] = control.RemoteCommandSocketPath(sess.Name)
	}
	if p, err := s.placementFor(sess); err == nil {
		for k, v := range s.approvalsEnv(sess, p.Bin) {
			env[k] = v
		}
	}
	return contracts.EncodeEnvSetting(env) + "\n"
}
