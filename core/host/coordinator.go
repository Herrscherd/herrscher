package host

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/manager"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
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
	List() ([]agent.Agent, error)
}
type cleanBrancher interface {
	IsCleanAt(path string) (bool, error)
	Branch(name string) string
	BranchExistsAt(path, branch string) (bool, error)
	MergeInto(leadPath, branch string) (worktree.MergeOutcome, []string, error)
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

	mu       sync.Mutex
	reported map[string]map[string]bool // parent → { worker → true } livrés
	expected map[string]int             // parent → N attendu (sous mu), figé par Seal
}

func newCoordinator(creator sessionCreator, agents agentLookup, wt cleanBrancher, sessions sessionLister, closer sessionCloser, seed func(string, string) bool) *coordinator {
	return &coordinator{
		creator: creator, agents: agents, wt: wt, sessions: sessions, closer: closer, seed: seed,
		reported: map[string]map[string]bool{},
		expected: map[string]int{},
	}
}

// findSession resolves a session by name in the current snapshot.
func (c *coordinator) findSession(name string) (state.Session, bool) {
	return findByName(c.sessions.SnapshotSessions(), name)
}

// CoordinationView is a read-only snapshot of a session's join state, projected
// from the coordinator's in-memory maps for observability (Neublox reads it via
// session list --json). It exposes only state the coordinator actually holds —
// no invented phase: reported/expected are the join counters, complete is the
// deterministic barrier (the same done>=N Report uses).
type CoordinationView struct {
	Role     string // "lead" | "worker"
	Lead     string // cohort identity: a worker's Parent, or a lead's own name
	Reported int    // workers that delivered (lead only; 0 for a worker)
	Expected int    // cohort size once sealed (lead only; 0 if unsealed)
	Complete bool   // Expected>0 && Reported>=Expected
}

// coordViewAdapter bridges the coordinator's CoordinationView to the manager's
// local coordinationReader interface (decouples manager from host types, no
// import cycle). Wired daemon-side in serve.go so session list --json dispatched
// through the live hub carries coordination.
type coordViewAdapter struct{ c *coordinator }

func (a coordViewAdapter) CoordinationView(name string) (manager.CoordView, bool) {
	v, ok := a.c.CoordinationView(name)
	if !ok {
		return manager.CoordView{}, false
	}
	return manager.CoordView{
		Role: v.Role, Lead: v.Lead, Reported: v.Reported,
		Expected: v.Expected, Complete: v.Complete,
	}, true
}

// CoordinationView projects name's join state. ok=false means the session has no
// coordination role (no cohort as a lead, no Parent as a worker) — a solo session.
func (c *coordinator) CoordinationView(name string) (CoordinationView, bool) {
	c.mu.Lock()
	reported, hasReported := c.reported[name]
	expected, hasExpected := c.expected[name]
	c.mu.Unlock()
	if hasReported || hasExpected {
		return CoordinationView{
			Role:     "lead",
			Lead:     name,
			Reported: len(reported),
			Expected: expected,
			Complete: expected > 0 && len(reported) >= expected,
		}, true
	}
	if sess, ok := c.findSession(name); ok && sess.Parent != "" {
		return CoordinationView{Role: "worker", Lead: sess.Parent}, true
	}
	return CoordinationView{}, false
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

// childCount returns how many sessions in the snapshot have parent as their
// Parent — a lead's live cohort size. Report, Seal, and FanOut all count a
// lead's children from the same snapshot, so they share this.
func childCount(sessions []state.Session, parent string) int {
	n := 0
	for _, s := range sessions {
		if s.Parent == parent {
			n++
		}
	}
	return n
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

	c.mu.Lock()
	if c.reported[from.Parent] == nil {
		c.reported[from.Parent] = map[string]bool{}
	}
	c.reported[from.Parent][req.FromSession] = true
	done := len(c.reported[from.Parent])
	sealed, isSealed := c.expected[from.Parent]
	c.mu.Unlock()

	total := 0
	if isSealed {
		total = sealed
	} else {
		total = childCount(sessions, from.Parent)
	}

	branch := c.wt.Branch(req.FromSession)
	msg := fmt.Sprintf("%s a terminé sur %s (%d/%d) — %s", req.FromSession, branch, done, total, req.Summary)
	if done >= total {
		if isSealed {
			msg += " — cohorte complète"
		} else {
			msg += " — tous les workers ont livré"
		}
	}
	if !c.seedWithRetry(ctx, from.Parent, msg) {
		return "", fmt.Errorf("report: delivery to parent %q timed out", from.Parent)
	}
	return from.Parent, nil
}

// Merge aggregates worker W's committed branch (session/<W>) into the lead's
// worktree via a real git merge, then seeds the lead the outcome. Lead-initiated
// (⟢ merge). Every guard runs before the side effect, on one atomic snapshot:
// lead known → worker known → worker is a child of THIS lead → worker committed →
// lead committed. Lead-clean is required so a conflict abort restores the lead's
// worktree without discarding uncommitted lead work. A conflict is not an error:
// MergeInto aborts (lead left clean) and the lead is seeded a diagnostic. W stays
// alive; the join state (reported/mu) is deliberately untouched — delivery
// tracking and aggregation are orthogonal.
func (c *coordinator) Merge(ctx context.Context, req contracts.MergeRequest) (string, error) {
	sessions := c.sessions.SnapshotSessions()
	lead, ok := findByName(sessions, req.FromSession)
	if !ok {
		return "", fmt.Errorf("merge: lead %q not found", req.FromSession)
	}
	worker, ok := findByName(sessions, req.Worker)
	if !ok {
		return "", fmt.Errorf("merge: worker %q not found", req.Worker)
	}
	if worker.Parent != req.FromSession {
		return "", fmt.Errorf("merge refused: %q is not a worker of %q", req.Worker, req.FromSession)
	}
	// Worker must be committed: session/<W>'s tip is what gets merged, so
	// uncommitted worker work would not be aggregated. Mirror the Report guard.
	wClean, err := c.wt.IsCleanAt(worker.Worktree)
	if err != nil {
		return "", fmt.Errorf("merge: %w", err)
	}
	if !wClean {
		return "", fmt.Errorf("merge refused: worker %q has uncommitted changes — commit first", req.Worker)
	}
	// Lead must be committed so a conflict abort restores it cleanly without
	// clobbering uncommitted lead work.
	lClean, err := c.wt.IsCleanAt(lead.Worktree)
	if err != nil {
		return "", fmt.Errorf("merge: %w", err)
	}
	if !lClean {
		return "", fmt.Errorf("merge refused: lead %q has uncommitted changes — commit first", req.FromSession)
	}

	outcome, conflicts, err := c.wt.MergeInto(lead.Worktree, c.wt.Branch(req.Worker))
	if err != nil {
		return "", fmt.Errorf("merge: %w", err)
	}
	var msg string
	switch outcome {
	case worktree.MergeApplied:
		msg = fmt.Sprintf("branche de %s mergée dans %s", req.Worker, req.FromSession)
	case worktree.MergeUpToDate:
		msg = fmt.Sprintf("%s déjà à jour dans %s", req.Worker, req.FromSession)
	case worktree.MergeConflict:
		msg = fmt.Sprintf("merge de %s refusé : conflit sur %s — résous manuellement",
			req.Worker, strings.Join(conflicts, ", "))
	}
	if !c.seedWithRetry(ctx, req.FromSession, msg) {
		return "", fmt.Errorf("merge: delivery to lead %q timed out", req.FromSession)
	}
	return req.FromSession, nil
}

// Seal records how many workers FromSession's cohort expects, turning the join's
// best-effort "all delivered" into a deterministic barrier: once sealed, Report
// reports "cohorte complète" only at done >= N. Guards run before any effect on
// one atomic snapshot: lead known → N > 0 → N >= current cohort size (refusing an
// under-seal at the source). Re-seal is last-wins. Seal does not seed into the
// lead's turn — like Delegate it changes state and fans a status; the barrier
// surfaces in later Report messages.
func (c *coordinator) Seal(ctx context.Context, req contracts.SealRequest) (string, error) {
	sessions := c.sessions.SnapshotSessions()
	if _, ok := findByName(sessions, req.FromSession); !ok {
		return "", fmt.Errorf("seal: lead %q not found", req.FromSession)
	}
	if req.Expected <= 0 {
		return "", fmt.Errorf("seal refused: expected must be > 0")
	}
	cohort := childCount(sessions, req.FromSession)
	if req.Expected < cohort {
		return "", fmt.Errorf("seal refused: expected %d below current cohort size %d", req.Expected, cohort)
	}
	c.mu.Lock()
	c.expected[req.FromSession] = req.Expected
	c.mu.Unlock()
	return req.FromSession, nil
}

// FanOut spawns one worker per task — all children of FromSession off its
// committed tip, all from ToAgent — then seals the cohort to its real size, the
// batch counterpart of Delegate. Guards run before any spawn: unknown agent and
// unknown lead fail with nothing created. Each task then goes through spawn, whose
// own lead-clean guard means a dirty lead yields zero workers on the first task. A
// spawn failure mid-batch is not rolled back: the workers already created are real
// committed sessions, so FanOut stops, seals to what was actually spawned, and
// returns those names alongside the error. The seal counts the lead's preexisting
// children (from the guard snapshot) plus the workers spawned here.
func (c *coordinator) FanOut(ctx context.Context, req contracts.FanOutRequest) ([]string, error) {
	if _, ok := c.agents.Get(req.ToAgent); !ok {
		return nil, fmt.Errorf("fanout: unknown agent %q", req.ToAgent)
	}
	sessions := c.sessions.SnapshotSessions()
	lead, ok := findByName(sessions, req.FromSession)
	if !ok {
		return nil, fmt.Errorf("fanout: lead %q not found", req.FromSession)
	}
	if len(req.Tasks) == 0 {
		return nil, fmt.Errorf("fanout: no tasks")
	}
	preexisting := childCount(sessions, req.FromSession)
	var spawned []string
	var spawnErr error
	for _, task := range req.Tasks {
		name, err := c.spawn(ctx, lead, req.ToAgent, task, req.FromSession)
		if err != nil {
			spawnErr = err
			break
		}
		spawned = append(spawned, name)
	}
	if len(spawned) > 0 {
		c.mu.Lock()
		c.expected[req.FromSession] = preexisting + len(spawned)
		c.mu.Unlock()
	}
	return spawned, spawnErr
}

// Route picks the best-matching agent for req.Task by a deterministic capability
// score (pickAgent over the agent roster's declared tags — no LLM enters here,
// Model O) and then delegates to it: the chosen worker is a child of the lead off
// its committed tip, the lead stays alive (spawn with parent = lead, like
// Delegate). Guards run before any spawn: an empty task, a missing lead, a roster
// error, or no matching agent each fail with nothing created. spawn's own
// lead-clean guard means a dirty lead yields no worker. Returns the chosen agent
// and the worker's session.
func (c *coordinator) Route(ctx context.Context, req contracts.RouteRequest) (string, string, error) {
	if strings.TrimSpace(req.Task) == "" {
		return "", "", fmt.Errorf("route: empty task")
	}
	from, ok := c.findSession(req.FromSession)
	if !ok {
		return "", "", fmt.Errorf("route: lead %q not found", req.FromSession)
	}
	roster, err := c.agents.List()
	if err != nil {
		return "", "", fmt.Errorf("route: %w", err)
	}
	chosen, ok := pickAgent(roster, req.Task)
	if !ok {
		return "", "", fmt.Errorf("route: no agent matches task")
	}
	session, err := c.spawn(ctx, from, chosen, req.Task, req.FromSession)
	if err != nil {
		return "", "", err
	}
	return chosen, session, nil
}

// forget purge l'état de join d'une session qui se ferme. Deux effets : si `name`
// était un lead, sa cohorte est jetée (anti-fuite mémoire) ; si `name` était un
// worker, il sort des livrés de son parent — sinon un worker livré puis fermé
// resterait compté dans `done` alors que `total` (frères vivants) a baissé, faussant
// le « tous les workers ont livré ». Garde l'invariant done ≤ total. Une cohorte
// devenue vide après ce retrait est elle-même jetée, pour ne laisser aucune entrée
// résiduelle sur un daemon longue durée.
func (c *coordinator) forget(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.reported, name)
	delete(c.expected, name)
	for parent, workers := range c.reported {
		delete(workers, name)
		if len(workers) == 0 {
			delete(c.reported, parent)
		}
	}
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
