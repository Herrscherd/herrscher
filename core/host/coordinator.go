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
	CreateCoordinated(context.Context, contracts.CreateSession, string) (string, error)
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

func newCoordinator(creator sessionCreator, agents agentLookup, wt cleanBrancher, sessions sessionLister, closer sessionCloser, seed func(string, string) bool) *coordinator {
	return &coordinator{
		creator: creator, agents: agents, wt: wt, sessions: sessions, closer: closer, seed: seed,
		reported: map[string]map[string]bool{},
		expected: map[string]int{},
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
	v, ok := a.c.CoordinationView(name)
	if !ok {
		return manager.CoordView{}, false
	}
	return manager.CoordView{
		Role: v.Role, Lead: v.Lead, Reported: v.Reported,
		Expected: v.Expected, Complete: v.Complete,
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
			Role:     "lead",
			Lead:     name,
			Reported: reportedCount,
			Expected: expected,
			Complete: expected > 0 && reportedCount >= expected,
		}, true
	}
	if sess, ok := c.findSession(name); ok && sess.Parent != "" {
		return CoordinationView{Role: "worker", Lead: sess.Parent}, true
	}
	return CoordinationView{}, false
}

func findByName(sessions []state.Session, name string) (state.Session, bool) {
	for _, s := range sessions {
		if s.Name == name {
			return s, true
		}
	}
	return state.Session{}, false
}

func childCount(sessions []state.Session, parent string) int {
	n := 0
	for _, s := range sessions {
		if s.Parent == parent {
			n++
		}
	}
	return n
}

func (c *coordinator) spawn(ctx context.Context, from state.Session, toAgent, task, parent, vendor, cmd string) (string, error) {
	if from.Worktree == "" {
		return "", fmt.Errorf("coordination: %w", &noIsolatedWorktreeError{Session: from.Name})
	}
	clean, err := c.wt.IsCleanAt(from.Worktree)
	if err != nil {
		return "", fmt.Errorf("coordination: %w", err)
	}
	if !clean {
		return "", fmt.Errorf("coordination refused: %w", &uncommittedError{Role: "session", Name: from.Name})
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
			return "", fmt.Errorf("coordination: %w", &freeNameExhaustedError{Base: base, Tries: maxSpawnNameProbes})
		}
		bName = fmt.Sprintf("%s-%d", base, n)
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
		return "", fmt.Errorf("handoff: %w", &UnknownError{Kind: "agent", Name: req.ToAgent})
	}
	from, ok := c.findSession(req.FromSession)
	if !ok {
		return "", fmt.Errorf("handoff: %w", &NotFoundError{Kind: "session", Name: req.FromSession})
	}
	return c.spawn(ctx, from, req.ToAgent, req.Task, "", "", "")
}

func (c *coordinator) Delegate(ctx context.Context, req contracts.DelegateRequest) (string, error) {
	target, ok := c.agents.Get(req.ToAgent)
	if !ok {
		return "", fmt.Errorf("delegate: %w", &UnknownError{Kind: "agent", Name: req.ToAgent})
	}
	from, ok := c.findSession(req.FromSession)
	if !ok {
		return "", fmt.Errorf("delegate: %w", &NotFoundError{Kind: "lead", Name: req.FromSession})
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
		return "", fmt.Errorf("report: %w", &UnknownError{Kind: "worker", Name: req.FromSession})
	}
	if from.Parent == "" {
		return "", fmt.Errorf("report: %w", &noParentError{Session: req.FromSession})
	}
	clean, err := c.wt.IsCleanAt(from.Worktree)
	if err != nil {
		return "", fmt.Errorf("report: %w", err)
	}
	if !clean {
		return "", fmt.Errorf("report refused: %w", &uncommittedError{Role: "session", Name: req.FromSession})
	}
	if _, ok := findByName(sessions, from.Parent); !ok {
		return "", fmt.Errorf("report: %w", &NotFoundError{Kind: "parent", Name: from.Parent})
	}

	c.mu.Lock()
	if c.reported[from.Parent] == nil {
		c.reported[from.Parent] = map[string]bool{}
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
		return "", fmt.Errorf("merge: %w", &NotFoundError{Kind: "lead", Name: req.FromSession})
	}
	worker, ok := findByName(sessions, req.Worker)
	if !ok {
		return "", fmt.Errorf("merge: %w", &NotFoundError{Kind: "worker", Name: req.Worker})
	}
	if worker.Parent != req.FromSession {
		return "", fmt.Errorf("merge refused: %w", &notAWorkerError{Worker: req.Worker, Lead: req.FromSession})
	}
	wClean, err := c.wt.IsCleanAt(worker.Worktree)
	if err != nil {
		return "", fmt.Errorf("merge: %w", err)
	}
	if !wClean {
		return "", fmt.Errorf("merge refused: %w", &uncommittedError{Role: "worker", Name: req.Worker})
	}
	lClean, err := c.wt.IsCleanAt(lead.Worktree)
	if err != nil {
		return "", fmt.Errorf("merge: %w", err)
	}
	if !lClean {
		return "", fmt.Errorf("merge refused: %w", &uncommittedError{Role: "lead", Name: req.FromSession})
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
		msg = fmt.Sprintf("merge de %s refusé : conflit sur %s, à résoudre manuellement",
			req.Worker, strings.Join(conflicts, ", "))
	}
	if !c.seedWithRetry(ctx, req.FromSession, msg) {
		return "", fmt.Errorf("merge: %w", &deliveryTimeoutError{Role: "lead", Name: req.FromSession})
	}
	return req.FromSession, nil
}

func (c *coordinator) Seal(ctx context.Context, req contracts.SealRequest) (string, error) {
	sessions := c.sessions.SnapshotSessions()
	if _, ok := findByName(sessions, req.FromSession); !ok {
		return "", fmt.Errorf("seal: %w", &NotFoundError{Kind: "lead", Name: req.FromSession})
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
		return nil, fmt.Errorf("fanout: %w", &UnknownError{Kind: "agent", Name: req.ToAgent})
	}
	sessions := c.sessions.SnapshotSessions()
	lead, ok := findByName(sessions, req.FromSession)
	if !ok {
		return nil, fmt.Errorf("fanout: %w", &NotFoundError{Kind: "lead", Name: req.FromSession})
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
		return "", "", fmt.Errorf("route: %w", &NotFoundError{Kind: "lead", Name: req.FromSession})
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
