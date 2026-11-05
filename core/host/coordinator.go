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

type sessionCreator interface {
	CreateCoordinated(context.Context, contracts.CreateSession, string) (contracts.Result, error)
}
type agentLookup interface {
	Get(name string) (agent.Agent, bool)
	List() ([]agent.Agent, error)
}
type cleanBrancher interface {
	IsCleanAt(ctx context.Context, path string) (bool, error)
	Branch(name string) string
	BranchExistsAt(ctx context.Context, path, branch string) (bool, error)
	MergeInto(ctx context.Context, leadPath, branch string) (worktree.MergeOutcome, []string, error)
}
type sessionLister interface {
	SnapshotSessions() []state.Session
}
type sessionCloser interface {
	Close(ctx context.Context, name string, force bool) (contracts.Result, error)
}

type coordinator struct {
	creator  sessionCreator
	agents   agentLookup
	wt       cleanBrancher
	sessions sessionLister
	closer   sessionCloser
	seed     func(session, task string) bool

	mu       sync.Mutex
	reported map[string]map[string]bool
	expected map[string]int
}

func newCoordinator(creator sessionCreator, agents agentLookup, worktrees cleanBrancher, sessions sessionLister, closer sessionCloser, seed func(string, string) bool) *coordinator {
	return &coordinator{
		creator: creator, agents: agents, wt: worktrees, sessions: sessions, closer: closer, seed: seed,
		reported: make(map[string]map[string]bool),
		expected: make(map[string]int),
	}
}

func (c *coordinator) findSession(name string) (state.Session, bool) {
	return findByName(c.sessions.SnapshotSessions(), name)
}

// CoordinationView is a read-only snapshot of a session's join state, projected
// from the coordinator's in-memory maps for observability (Neublox reads it via
// session list --json). It exposes only state the coordinator actually holds.
// No invented phase: reported/expected are the join counters, complete is the
// deterministic barrier (the same done>=N Report uses).
type CoordinationView struct {
	Role     string
	Lead     string
	Reported int
	Expected int
	Complete bool
}

type coordViewAdapter struct{ c *coordinator }

func (a coordViewAdapter) CoordinationView(name string) (manager.CoordView, bool) {
	view, ok := a.c.CoordinationView(name)
	if !ok {
		return manager.CoordView{}, false
	}
	return manager.CoordView{
		Role: view.Role, Lead: view.Lead, Reported: view.Reported,
		Expected: view.Expected, Complete: view.Complete,
	}, true
}

func (c *coordinator) CoordinationView(name string) (CoordinationView, bool) {
	c.mu.Lock()
	_, hasReported := c.reported[name]
	expected, hasExpected := c.expected[name]
	reportedCount := len(c.reported[name])
	c.mu.Unlock()
	if hasReported || hasExpected {
		return CoordinationView{
			Role:     roleLead,
			Lead:     name,
			Reported: reportedCount,
			Expected: expected,
			Complete: expected > 0 && reportedCount >= expected,
		}, true
	}
	if sess, ok := c.findSession(name); ok && sess.Parent != "" {
		return CoordinationView{Role: roleWorker, Lead: sess.Parent}, true
	}
	return CoordinationView{}, false
}

func findByName(sessions []state.Session, name string) (state.Session, bool) {
	for idx := range sessions {
		found := sessions[idx]
		if found.Name == name {
			return found, true
		}
	}
	return state.Session{}, false
}

func childCount(sessions []state.Session, parent string) int {
	count := 0
	for idx := range sessions {
		s := sessions[idx]
		if s.Parent == parent {
			count++
		}
	}
	return count
}

func (c *coordinator) spawn(ctx context.Context, from state.Session, toAgent, task, parent, vendor, cmd string) (string, error) {
	if from.Worktree == "" {
		return "", fmt.Errorf("coordination: %w", &noIsolatedWorktreeError{Session: from.Name})
	}
	clean, err := c.wt.IsCleanAt(ctx, from.Worktree)
	if err != nil {
		return "", fmt.Errorf("coordination: %w", err)
	}
	if !clean {
		return "", fmt.Errorf("coordination refused: %w", &uncommittedError{Role: roleSession, Name: from.Name})
	}

	base := from.Name + "-" + toAgent
	bName := base
	for attempt := 2; ; attempt++ {
		exists, err := c.wt.BranchExistsAt(ctx, from.Worktree, c.wt.Branch(bName))
		if err != nil {
			return "", fmt.Errorf("coordination: %w", err)
		}
		if !exists {
			break
		}
		if attempt > maxSpawnNameProbes {
			return "", fmt.Errorf("coordination: %w", &freeNameExhaustedError{Base: base, Tries: maxSpawnNameProbes})
		}
		bName = fmt.Sprintf("%s-%d", base, attempt)
	}

	if _, err := c.creator.CreateCoordinated(ctx, contracts.CreateSession{
		Name:    bName,
		Project: from.Project,
		Cmd:     cmd,
		Agent:   toAgent,
		Base:    c.wt.Branch(from.Name),
		Parent:  parent,
	}, vendor); err != nil {
		return "", fmt.Errorf("coordination: create %q: %w", bName, err)
	}
	if !c.seedWithRetry(ctx, bName, task) {
		_, rollbackErr := c.closer.Close(context.WithoutCancel(ctx), bName, true)
		logCleanup(rollbackErr)
		return "", fmt.Errorf("coordination: %w", &seedTimedOutError{Session: bName})
	}
	return bName, nil
}

func (c *coordinator) Handoff(ctx context.Context, req contracts.HandoffRequest) (string, error) {
	if _, ok := c.agents.Get(req.ToAgent); !ok {
		return "", fmt.Errorf("handoff: %w", &UnknownError{Kind: kindAgent, Name: req.ToAgent})
	}
	from, ok := c.findSession(req.FromSession)
	if !ok {
		return "", fmt.Errorf("handoff: %w", &NotFoundError{Kind: kindSession, Name: req.FromSession})
	}
	return c.spawn(ctx, from, req.ToAgent, req.Task, "", "", "")
}

func (c *coordinator) Delegate(ctx context.Context, req contracts.DelegateRequest) (string, error) {
	target, ok := c.agents.Get(req.ToAgent)
	if !ok {
		return "", fmt.Errorf("delegate: %w", &UnknownError{Kind: kindAgent, Name: req.ToAgent})
	}
	from, ok := c.findSession(req.FromSession)
	if !ok {
		return "", fmt.Errorf("delegate: %w", &NotFoundError{Kind: kindLead, Name: req.FromSession})
	}
	vendor, cmd := "", ""
	if target.Backend == "" {
		vendor = from.Vendor
		if target.Cmd == "" {
			cmd = from.Cmd
		}
	}
	return c.spawn(ctx, from, req.ToAgent, req.Task, req.FromSession, vendor, cmd)
}

func (c *coordinator) Report(ctx context.Context, req contracts.ReportRequest) (string, error) {
	sessions := c.sessions.SnapshotSessions()
	from, ok := findByName(sessions, req.FromSession)
	if !ok {
		return "", fmt.Errorf("report: %w", &UnknownError{Kind: kindWorker, Name: req.FromSession})
	}
	if from.Parent == "" {
		return "", fmt.Errorf("report: %w", &noParentError{Session: req.FromSession})
	}
	clean, err := c.wt.IsCleanAt(ctx, from.Worktree)
	if err != nil {
		return "", fmt.Errorf("report: %w", err)
	}
	if !clean {
		return "", fmt.Errorf("report refused: %w", &uncommittedError{Role: roleSession, Name: req.FromSession})
	}
	if _, ok := findByName(sessions, from.Parent); !ok {
		return "", fmt.Errorf("report: %w", &NotFoundError{Kind: "parent", Name: from.Parent})
	}

	c.mu.Lock()
	if c.reported[from.Parent] == nil {
		c.reported[from.Parent] = make(map[string]bool)
	}
	c.reported[from.Parent][req.FromSession] = true
	done := len(c.reported[from.Parent])
	sealed, isSealed := c.expected[from.Parent]
	c.mu.Unlock()

	var total int
	if isSealed {
		total = sealed
	} else {
		total = childCount(sessions, from.Parent)
	}

	branch := c.wt.Branch(req.FromSession)
	msg := fmt.Sprintf("%s a terminé sur %s (%d/%d): %s", req.FromSession, branch, done, total, req.Summary)
	if done >= total {
		if isSealed {
			msg += ", cohorte complète"
		} else {
			msg += ", tous les workers ont livré"
		}
	}
	if !c.seedWithRetry(ctx, from.Parent, msg) {
		return "", fmt.Errorf("report: %w", &deliveryTimeoutError{Role: "parent", Name: from.Parent})
	}
	return from.Parent, nil
}

func (c *coordinator) Merge(ctx context.Context, req contracts.MergeRequest) (string, error) {
	sessions := c.sessions.SnapshotSessions()
	lead, ok := findByName(sessions, req.FromSession)
	if !ok {
		return "", fmt.Errorf("merge: %w", &NotFoundError{Kind: kindLead, Name: req.FromSession})
	}
	worker, ok := findByName(sessions, req.Worker)
	if !ok {
		return "", fmt.Errorf("merge: %w", &NotFoundError{Kind: kindWorker, Name: req.Worker})
	}
	if worker.Parent != req.FromSession {
		return "", fmt.Errorf("merge refused: %w", &notAWorkerError{Worker: req.Worker, Lead: req.FromSession})
	}
	wClean, err := c.wt.IsCleanAt(ctx, worker.Worktree)
	if err != nil {
		return "", fmt.Errorf("merge: %w", err)
	}
	if !wClean {
		return "", fmt.Errorf("merge refused: %w", &uncommittedError{Role: roleWorker, Name: req.Worker})
	}
	lClean, err := c.wt.IsCleanAt(ctx, lead.Worktree)
	if err != nil {
		return "", fmt.Errorf("merge: %w", err)
	}
	if !lClean {
		return "", fmt.Errorf("merge refused: %w", &uncommittedError{Role: roleLead, Name: req.FromSession})
	}

	outcome, conflicts, err := c.wt.MergeInto(ctx, lead.Worktree, c.wt.Branch(req.Worker))
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
		msg = fmt.Sprintf("merge de %s refusé : conflit sur %s, à résoudre manuellement",
			req.Worker, strings.Join(conflicts, ", "))
	}
	if !c.seedWithRetry(ctx, req.FromSession, msg) {
		return "", fmt.Errorf("merge: %w", &deliveryTimeoutError{Role: roleLead, Name: req.FromSession})
	}
	return req.FromSession, nil
}

func (c *coordinator) Seal(_ context.Context, req contracts.SealRequest) (string, error) {
	sessions := c.sessions.SnapshotSessions()
	if _, ok := findByName(sessions, req.FromSession); !ok {
		return "", fmt.Errorf("seal: %w", &NotFoundError{Kind: kindLead, Name: req.FromSession})
	}
	if req.Expected <= 0 {
		return "", errSealExpectedPositive
	}
	cohort := childCount(sessions, req.FromSession)
	if req.Expected < cohort {
		return "", fmt.Errorf("seal refused: %w", &sealBelowCohortError{Expected: req.Expected, Cohort: cohort})
	}
	c.mu.Lock()
	c.expected[req.FromSession] = req.Expected
	c.mu.Unlock()
	return req.FromSession, nil
}

func (c *coordinator) FanOut(ctx context.Context, req contracts.FanOutRequest) ([]string, error) {
	if _, ok := c.agents.Get(req.ToAgent); !ok {
		return nil, fmt.Errorf("fanout: %w", &UnknownError{Kind: kindAgent, Name: req.ToAgent})
	}
	sessions := c.sessions.SnapshotSessions()
	lead, ok := findByName(sessions, req.FromSession)
	if !ok {
		return nil, fmt.Errorf("fanout: %w", &NotFoundError{Kind: kindLead, Name: req.FromSession})
	}
	if len(req.Tasks) == 0 {
		return nil, errFanoutNoTasks
	}
	preexisting := childCount(sessions, req.FromSession)
	var spawned []string
	var spawnErr error
	for _, task := range req.Tasks {
		name, err := c.spawn(ctx, lead, req.ToAgent, task, req.FromSession, "", "")
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

func (c *coordinator) Route(ctx context.Context, req contracts.RouteRequest) (string, string, error) {
	if strings.TrimSpace(req.Task) == "" {
		return "", "", errRouteEmptyTask
	}
	from, ok := c.findSession(req.FromSession)
	if !ok {
		return "", "", fmt.Errorf("route: %w", &NotFoundError{Kind: kindLead, Name: req.FromSession})
	}
	roster, err := c.agents.List()
	if err != nil {
		return "", "", fmt.Errorf("route: %w", err)
	}
	chosen, ok := pickAgent(roster, req.Task)
	if !ok {
		return "", "", errRouteNoAgent
	}
	session, err := c.spawn(ctx, from, chosen, req.Task, req.FromSession, "", "")
	if err != nil {
		return "", "", err
	}
	return chosen, session, nil
}

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

func (c *coordinator) seedWithRetry(ctx context.Context, session, task string) bool {
	for range seedAttempts {
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
