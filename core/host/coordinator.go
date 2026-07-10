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
	seedAttempts = 50
	seedBackoff  = 100 * time.Millisecond
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
}
type sessionLister interface {
	SnapshotSessions() []state.Session
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
	seed     func(session, task string) bool
}

func newCoordinator(creator sessionCreator, agents agentLookup, wt cleanBrancher, sessions sessionLister, seed func(string, string) bool) *coordinator {
	return &coordinator{creator: creator, agents: agents, wt: wt, sessions: sessions, seed: seed}
}

// Handoff creates B continuing FromSession's committed work, seeds Task as B's
// opening turn, and returns B's name. Order matters: every guard runs before any
// side effect, so a rejected handoff leaves nothing partial.
func (c *coordinator) Handoff(ctx context.Context, req contracts.HandoffRequest) (string, error) {
	if _, ok := c.agents.Get(req.ToAgent); !ok {
		return "", fmt.Errorf("handoff: unknown agent %q", req.ToAgent)
	}
	var from *state.Session
	for i := range c.sessions.SnapshotSessions() {
		if s := c.sessions.SnapshotSessions()[i]; s.Name == req.FromSession {
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

	bName := req.FromSession + "-" + req.ToAgent
	if _, err := c.creator.Create(ctx, contracts.CreateSession{
		Name:    bName,
		Project: from.Project,
		Agent:   req.ToAgent,
		Base:    c.wt.Branch(req.FromSession),
	}); err != nil {
		return "", fmt.Errorf("handoff: create %q: %w", bName, err)
	}
	if !c.seedWithRetry(bName, req.Task) {
		return bName, fmt.Errorf("handoff: session %q created but seeding timed out", bName)
	}
	return bName, nil
}

// seedWithRetry waits for B's driver to register (goLive starts RunSession in a
// goroutine) before enqueuing the task, bounded so a never-arriving session
// surfaces as a timeout instead of hanging.
func (c *coordinator) seedWithRetry(session, task string) bool {
	for i := 0; i < seedAttempts; i++ {
		if c.seed(session, task) {
			return true
		}
		time.Sleep(seedBackoff)
	}
	return false
}
