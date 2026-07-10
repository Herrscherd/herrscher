package host

import (
	"context"
	"fmt"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	seedAttempts         = 50
	seedBackoff          = 100 * time.Millisecond
	maxHandoffNameProbes = 100
)

// Small ports the coordinator depends on, each satisfied by an existing host
// component (*hub, *agent.Store, *worktree.Worktreer, *state.State). Kept tiny so
// Handoff is testable with fakes and no real git/session machinery.
type sessionCreator interface {
	Create(context.Context, contracts.CreateSession) (string, error)
}
type agentLookup interface {
	Get(name string) (agent.Agent, bool)
}
type cleanBrancher interface {
	IsCleanAt(path string) (bool, error)
	Branch(name string) string
	BranchExistsAt(path, branch string) (bool, error)
}
type sessionLister interface {
	SnapshotSessions() []state.Session
}
type sessionCloser interface {
	Close(ctx context.Context, name string, force bool) (string, error)
}

// coordinator implements contracts.Coordinator at the host layer: it sees every
// session (sessionLister) and drives the hub (sessionCreator), which the
// per-session Orchestrator plugin cannot. Model O: the agent only signals; here
// is where the decision and execution live.
type coordinator struct {
	creator  sessionCreator
	agents   agentLookup
	wt       cleanBrancher
	sessions sessionLister
	closer   sessionCloser
	seed     func(session, task string) bool
}

func newCoordinator(creator sessionCreator, agents agentLookup, wt cleanBrancher, sessions sessionLister, closer sessionCloser, seed func(string, string) bool) *coordinator {
	return &coordinator{creator: creator, agents: agents, wt: wt, sessions: sessions, closer: closer, seed: seed}
}

// Handoff creates B continuing FromSession's committed work, seeds Task as B's
// opening turn, and returns B's name. Order matters: every guard runs before any
// side effect, so a rejected handoff leaves nothing partial.
func (c *coordinator) Handoff(ctx context.Context, req contracts.HandoffRequest) (string, error) {
	if _, ok := c.agents.Get(req.ToAgent); !ok {
		return "", fmt.Errorf("handoff: unknown agent %q", req.ToAgent)
	}
	sessions := c.sessions.SnapshotSessions()
	var from *state.Session
	for i := range sessions {
		if s := sessions[i]; s.Name == req.FromSession {
			from = &s
			break
		}
	}
	if from == nil {
		return "", fmt.Errorf("handoff: source session %q not found", req.FromSession)
	}
	if from.Worktree == "" {
		return "", fmt.Errorf("handoff: source session %q has no isolated worktree to continue", req.FromSession)
	}
	clean, err := c.wt.IsCleanAt(from.Worktree)
	if err != nil {
		return "", fmt.Errorf("handoff: %w", err)
	}
	if !clean {
		return "", fmt.Errorf("handoff refused: session %q has uncommitted changes — commit first", req.FromSession)
	}

	base := req.FromSession + "-" + req.ToAgent
	bName := base
	for n := 2; ; n++ {
		exists, err := c.wt.BranchExistsAt(from.Worktree, c.wt.Branch(bName))
		if err != nil {
			return "", fmt.Errorf("handoff: %w", err)
		}
		if !exists {
			break
		}
		if n > maxHandoffNameProbes {
			return "", fmt.Errorf("handoff: no free session name for %q after %d tries", base, maxHandoffNameProbes)
		}
		bName = fmt.Sprintf("%s-%d", base, n)
	}

	if _, err := c.creator.Create(ctx, contracts.CreateSession{
		Name:    bName,
		Project: from.Project,
		Agent:   req.ToAgent,
		Base:    c.wt.Branch(req.FromSession),
	}); err != nil {
		return "", fmt.Errorf("handoff: create %q: %w", bName, err)
	}
	if !c.seedWithRetry(ctx, bName, req.Task) {
		// B was created but never came live to receive the task: roll it back
		// rather than leave an orphan worktree/branch/driver. force:true — B has
		// no committed work of its own yet (it only carries A's base tip).
		// Use a ctx detached from cancellation: seedWithRetry can return false
		// BECAUSE ctx was cancelled, and if Close ran with that same dead ctx its
		// Archive/RemoveSession steps could bail early, leaving B's state row and
		// channel behind despite the "rolled back" error below.
		_, _ = c.closer.Close(context.WithoutCancel(ctx), bName, true)
		return "", fmt.Errorf("handoff: session %q created but seeding timed out; rolled back", bName)
	}
	return bName, nil
}

// seedWithRetry waits for B's driver to register (goLive starts RunSession in a
// goroutine) before enqueuing the task, bounded so a never-arriving session
// surfaces as a timeout instead of hanging.
func (c *coordinator) seedWithRetry(ctx context.Context, session, task string) bool {
	for i := 0; i < seedAttempts; i++ {
		if c.seed(session, task) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(seedBackoff):
		}
	}
	return false
}
