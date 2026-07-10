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
	seedAttempts       = 50
	seedBackoff        = 100 * time.Millisecond
	maxSpawnNameProbes = 100
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

// findSession resolves a session by name in the current snapshot.
func (c *coordinator) findSession(name string) (state.Session, bool) {
	return findByName(c.sessions.SnapshotSessions(), name)
}

// findByName resolves a session by name in an already-taken snapshot, so a
// caller needing two lookups (worker + parent) reads both from one atomic view.
func findByName(sessions []state.Session, name string) (state.Session, bool) {
	for _, s := range sessions {
		if s.Name == name {
			return s, true
		}
	}
	return state.Session{}, false
}

// spawn creates a new session branched off `from`'s committed tip, seeds `task`
// as its opening turn, and records `parent` on it (empty for a handoff, the
// lead's name for a delegation). Order matters: every guard runs before any side
// effect. The name is probed collision-free because worktree.Remove leaves the
// session/<name> branch intact, so a reused deterministic name would collide.
// On a seed timeout the fresh session is rolled back rather than left orphaned.
func (c *coordinator) spawn(ctx context.Context, from state.Session, toAgent, task, parent string) (string, error) {
	if from.Worktree == "" {
		return "", fmt.Errorf("coordination: source session %q has no isolated worktree to continue", from.Name)
	}
	clean, err := c.wt.IsCleanAt(from.Worktree)
	if err != nil {
		return "", fmt.Errorf("coordination: %w", err)
	}
	if !clean {
		return "", fmt.Errorf("coordination refused: session %q has uncommitted changes — commit first", from.Name)
	}

	base := from.Name + "-" + toAgent
	bName := base
	for n := 2; ; n++ {
		exists, err := c.wt.BranchExistsAt(from.Worktree, c.wt.Branch(bName))
		if err != nil {
			return "", fmt.Errorf("coordination: %w", err)
		}
		if !exists {
			break
		}
		if n > maxSpawnNameProbes {
			return "", fmt.Errorf("coordination: no free session name for %q after %d tries", base, maxSpawnNameProbes)
		}
		bName = fmt.Sprintf("%s-%d", base, n)
	}

	if _, err := c.creator.Create(ctx, contracts.CreateSession{
		Name:    bName,
		Project: from.Project,
		Agent:   toAgent,
		Base:    c.wt.Branch(from.Name),
		Parent:  parent,
	}); err != nil {
		return "", fmt.Errorf("coordination: create %q: %w", bName, err)
	}
	if !c.seedWithRetry(ctx, bName, task) {
		// The session was created but never came live to receive the task: roll it
		// back rather than leave an orphan worktree/branch/driver. force:true — it
		// has no committed work of its own yet (it only carries the base tip).
		// Use a ctx detached from cancellation: seedWithRetry can return false
		// BECAUSE ctx was cancelled, and if Close ran with that same dead ctx its
		// Archive/RemoveSession steps could bail early, leaving the state row and
		// channel behind despite the "rolled back" error below.
		_, _ = c.closer.Close(context.WithoutCancel(ctx), bName, true)
		return "", fmt.Errorf("coordination: session %q created but seeding timed out; rolled back", bName)
	}
	return bName, nil
}

// Handoff creates B continuing FromSession's committed work, seeds Task as B's
// opening turn, and returns B's name. B has no parent — a handoff is a one-shot
// relay: A finishes, there is no result-back.
func (c *coordinator) Handoff(ctx context.Context, req contracts.HandoffRequest) (string, error) {
	if _, ok := c.agents.Get(req.ToAgent); !ok {
		return "", fmt.Errorf("handoff: unknown agent %q", req.ToAgent)
	}
	from, ok := c.findSession(req.FromSession)
	if !ok {
		return "", fmt.Errorf("handoff: source session %q not found", req.FromSession)
	}
	return c.spawn(ctx, from, req.ToAgent, req.Task, "")
}

// Delegate creates a worker W off the lead's committed tip and records the lead
// as W's parent for the result-back channel (Report). The key difference from a
// handoff: parent is set, and the lead stays alive (spawn touches only W).
func (c *coordinator) Delegate(ctx context.Context, req contracts.DelegateRequest) (string, error) {
	if _, ok := c.agents.Get(req.ToAgent); !ok {
		return "", fmt.Errorf("delegate: unknown agent %q", req.ToAgent)
	}
	from, ok := c.findSession(req.FromSession)
	if !ok {
		return "", fmt.Errorf("delegate: lead session %q not found", req.FromSession)
	}
	return c.spawn(ctx, from, req.ToAgent, req.Task, req.FromSession)
}

// Report delivers a worker's completion to its lead: {session/<W> branch ref +
// summary}. No merge, no teardown — W stays alive. Delivery reuses the same seed
// channel as a session opening (Model O, host layer). Every guard runs before
// the side effect: unknown worker, worker with no parent, an uncommitted worker,
// or a parent no longer present each fail with nothing delivered. Worker and
// parent are resolved from one snapshot so the view is atomic.
func (c *coordinator) Report(ctx context.Context, req contracts.ReportRequest) (string, error) {
	sessions := c.sessions.SnapshotSessions()
	from, ok := findByName(sessions, req.FromSession)
	if !ok {
		return "", fmt.Errorf("report: unknown worker %q", req.FromSession)
	}
	if from.Parent == "" {
		return "", fmt.Errorf("report: session %q has no parent — nothing to deliver", req.FromSession)
	}
	// W must be committed: the delivered ref is session/<W>'s tip, so uncommitted
	// work would hand the lead a branch that carries none of it. Refuse (mirrors
	// spawn's clean guard). An empty worktree fails IsCleanAt, caught here too.
	clean, err := c.wt.IsCleanAt(from.Worktree)
	if err != nil {
		return "", fmt.Errorf("report: %w", err)
	}
	if !clean {
		return "", fmt.Errorf("report refused: session %q has uncommitted changes — commit first", req.FromSession)
	}
	if _, ok := findByName(sessions, from.Parent); !ok {
		return "", fmt.Errorf("report: parent %q of %q not found", from.Parent, req.FromSession)
	}
	branch := c.wt.Branch(req.FromSession)
	msg := fmt.Sprintf("%s a terminé sur %s — %s", req.FromSession, branch, req.Summary)
	if !c.seedWithRetry(ctx, from.Parent, msg) {
		return "", fmt.Errorf("report: delivery to parent %q timed out", from.Parent)
	}
	return from.Parent, nil
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
