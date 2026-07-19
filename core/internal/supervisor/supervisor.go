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
	cancels map[string]context.CancelFunc
	log     *slog.Logger
	// sleep and now are clock seams (default time.After / time.Now) so tests can
	// drive the restart loop and its backoff without real wall-clock waits.
	sleep func(time.Duration) <-chan time.Time
	now   func() time.Time
	// metrics records bridge-restart counts (nil = no recording, e.g. in tests).
	metrics *metrics.Registry
}

// bridgeArgs builds the child `herrscher bridge` argv for sess.
func (s *Supervisor) bridgeArgs(sess state.Session) []string {
	args := []string{"bridge", "-c", sess.ChannelID, "--cmd", sess.Cmd, "--session", sess.Name,
		"--hub-socket", control.SocketPath(sess.Name)}
	// P1: thread the session's memory scope so the orchestrator recalls the
	// game's shared memory and this agent's private skills each turn.
	if sess.Project != "" {
		args = append(args, "--project", sess.Project)
	}
	if sess.Agent != "" {
		args = append(args, "--agent", sess.Agent)
	}
	if sess.Backend != "" && sess.Backend != "stream" {
		args = append(args, "--backend", sess.Backend)
	}
	if sess.Vendor != "" {
		args = append(args, "--vendor", sess.Vendor)
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
	return args
}

// NewSupervisor builds a Supervisor bound to ctx. It logs through a quiet
// default until SetLogger installs the daemon's operator logger.
func NewSupervisor(ctx context.Context, selfBin string) *Supervisor {
	return &Supervisor{
		ctx:     ctx,
		selfBin: selfBin,
		cancels: map[string]context.CancelFunc{},
		log:     obs.NewLogger(os.Stderr, slog.LevelInfo),
		sleep:   time.After,
		now:     time.Now,
	}
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

// Start launches a supervised bridge for sess (idempotent per name).
func (s *Supervisor) Start(sess state.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, running := s.cancels[sess.Name]; running {
		return nil
	}
	cctx, cancel := context.WithCancel(s.ctx)
	s.cancels[sess.Name] = cancel
	go s.runLoop(cctx, sess)
	return nil
}

// Stop terminates the bridge for name.
func (s *Supervisor) Stop(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.cancels[name]; ok {
		cancel()
		delete(s.cancels, name)
	}
	return nil
}

func (s *Supervisor) runLoop(ctx context.Context, sess state.Session) {
	bo := obs.RestartBackoff()
	for {
		if ctx.Err() != nil {
			return
		}
		cmd := exec.CommandContext(ctx, s.selfBin, s.bridgeArgs(sess)...)
		// Dir is the resolved run directory (worktree, or workspace/project root);
		// fall back to Worktree for sessions persisted before Dir existed. Empty
		// leaves cmd.Dir unset so the child inherits the launcher's cwd.
		if dir := sess.Dir; dir != "" {
			cmd.Dir = dir
		} else if sess.Worktree != "" {
			cmd.Dir = sess.Worktree
		}
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		cmd.Env = os.Environ()
		start := s.now()
		_ = cmd.Run() // returns on exit or ctx cancel
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
