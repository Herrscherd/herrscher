package host

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

type fakeCreator struct {
	spec contracts.CreateSession
	err  error
}

func (f *fakeCreator) Create(_ context.Context, s contracts.CreateSession) (string, error) {
	f.spec = s
	return s.Name, f.err
}

type fakeAgents struct{ known map[string]bool }

func (f fakeAgents) Get(name string) (agent.Agent, bool) {
	return agent.Agent{}, f.known[name]
}

type fakeWTC struct {
	clean    bool
	err      error
	branches map[string]bool
	// cleanBy overrides `clean` per worktree path when non-nil (path → clean?).
	cleanBy map[string]bool
	// merge scripts MergeInto's return; mergedBranch records the branch merged.
	merge        worktree.MergeOutcome
	mergeConf    []string
	mergeErr     error
	mergedBranch string
}

func (f *fakeWTC) IsCleanAt(path string) (bool, error) {
	if f.cleanBy != nil {
		return f.cleanBy[path], f.err
	}
	return f.clean, f.err
}
func (f *fakeWTC) Branch(name string) string { return "session/" + name }
func (f *fakeWTC) BranchExistsAt(_, branch string) (bool, error) {
	return f.branches[branch], nil
}
func (f *fakeWTC) MergeInto(_, branch string) (worktree.MergeOutcome, []string, error) {
	f.mergedBranch = branch
	return f.merge, f.mergeConf, f.mergeErr
}

type fakeSessions struct{ list []state.Session }

func (f fakeSessions) SnapshotSessions() []state.Session { return f.list }

type fakeCloser struct{ closed []string }

func (f *fakeCloser) Close(_ context.Context, name string, _ bool) (string, error) {
	f.closed = append(f.closed, name)
	return "", nil
}

func newTestCoordinator(cr *fakeCreator, known []string, clean bool, sessions []state.Session, seeded *[]string) *coordinator {
	return newTestCoordinatorFull(cr, known, clean, sessions, seeded, nil, &fakeCloser{})
}

func newTestCoordinatorFull(cr *fakeCreator, known []string, clean bool, sessions []state.Session, seeded *[]string, branches map[string]bool, closer *fakeCloser) *coordinator {
	km := map[string]bool{}
	for _, k := range known {
		km[k] = true
	}
	seed := func(sess, task string) bool { *seeded = append(*seeded, sess+"|"+task); return true }
	return newCoordinator(cr, fakeAgents{known: km}, &fakeWTC{clean: clean, branches: branches}, fakeSessions{list: sessions}, closer, seed)
}

func TestHandoffCreatesBOnABranchAndSeeds(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, true,
		[]state.Session{{Name: "alpha", Project: "game", Worktree: "/wt/alpha"}}, &seeded)

	name, err := c.Handoff(context.Background(), contracts.HandoffRequest{
		FromSession: "alpha", ToAgent: "scripter", Task: "finir le module",
	})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if cr.spec.Base != "session/alpha" {
		t.Fatalf("B not based on A's branch: %q", cr.spec.Base)
	}
	if cr.spec.Agent != "scripter" || cr.spec.Project != "game" {
		t.Fatalf("bad spec: %+v", cr.spec)
	}
	if len(seeded) != 1 || !strings.HasSuffix(seeded[0], "|finir le module") {
		t.Fatalf("task not seeded: %v", seeded)
	}
	if seeded[0] != name+"|finir le module" {
		t.Fatalf("seed targeted wrong session: %v (name=%s)", seeded, name)
	}
}

func TestHandoffUnknownAgent(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, nil, true,
		[]state.Session{{Name: "alpha", Worktree: "/wt/alpha"}}, &seeded)
	if _, err := c.Handoff(context.Background(), contracts.HandoffRequest{
		FromSession: "alpha", ToAgent: "ghost", Task: "x",
	}); err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if cr.spec.Name != "" {
		t.Fatal("no session should have been created")
	}
}

func TestHandoffDirtySourceRefused(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, false, // dirty
		[]state.Session{{Name: "alpha", Worktree: "/wt/alpha"}}, &seeded)
	if _, err := c.Handoff(context.Background(), contracts.HandoffRequest{
		FromSession: "alpha", ToAgent: "scripter", Task: "x",
	}); err == nil {
		t.Fatal("expected refusal for dirty source worktree")
	}
	if cr.spec.Name != "" {
		t.Fatal("no session should have been created")
	}
}

func TestHandoffUnknownSource(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, true, nil, &seeded)
	if _, err := c.Handoff(context.Background(), contracts.HandoffRequest{
		FromSession: "ghost", ToAgent: "scripter", Task: "x",
	}); err == nil {
		t.Fatal("expected error for missing source session")
	}
	if cr.spec.Name != "" {
		t.Fatal("no session should have been created")
	}
}

func TestHandoffPicksFreeNameWhenBranchExists(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	branches := map[string]bool{"session/alpha-scripter": true}
	c := newTestCoordinatorFull(cr, []string{"scripter"}, true,
		[]state.Session{{Name: "alpha", Project: "game", Worktree: "/wt/alpha"}}, &seeded, branches, &fakeCloser{})

	name, err := c.Handoff(context.Background(), contracts.HandoffRequest{
		FromSession: "alpha", ToAgent: "scripter", Task: "finir le module",
	})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if name != "alpha-scripter-2" {
		t.Fatalf("expected collision-free name alpha-scripter-2, got %q", name)
	}
	if cr.spec.Name != "alpha-scripter-2" {
		t.Fatalf("B not created with collision-free name: %q", cr.spec.Name)
	}
	if len(seeded) != 1 || seeded[0] != "alpha-scripter-2|finir le module" {
		t.Fatalf("seed did not target collision-free name: %v", seeded)
	}
}

func TestHandoffRollsBackOnSeedTimeout(t *testing.T) {
	cr := &fakeCreator{}
	closer := &fakeCloser{}
	km := map[string]bool{"scripter": true}
	seedFunc := func(sess, task string) bool { return false }
	c := newCoordinator(cr, fakeAgents{known: km}, &fakeWTC{clean: true}, fakeSessions{list: []state.Session{
		{Name: "alpha", Project: "game", Worktree: "/wt/alpha"},
	}}, closer, seedFunc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	name, err := c.Handoff(ctx, contracts.HandoffRequest{
		FromSession: "alpha", ToAgent: "scripter", Task: "finir le module",
	})
	if err == nil {
		t.Fatal("expected error for seed timeout")
	}
	if name != "" {
		t.Fatalf("expected empty name on rollback, got %q", name)
	}
	if len(closer.closed) != 1 || closer.closed[0] != "alpha-scripter" {
		t.Fatalf("expected rollback close of alpha-scripter, got %v", closer.closed)
	}
}

// TestSeedWithRetryHonoursCtxCancel pins seedWithRetry's ctx-awareness: with a
// pre-cancelled ctx it must stop after exactly one seed attempt (the loop tries
// c.seed once, then hits the <-ctx.Done() case on its first select). If the
// ctx select were reverted to a plain sleep, this would instead run all
// seedAttempts (50) tries — this test would then fail on the count assertion
// instead of hanging for the full 50*100ms, so it stays fast either way.
func TestSeedWithRetryHonoursCtxCancel(t *testing.T) {
	var attempts int
	seedFunc := func(sess, task string) bool { attempts++; return false }
	c := newCoordinator(&fakeCreator{}, fakeAgents{}, &fakeWTC{}, fakeSessions{}, &fakeCloser{}, seedFunc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if ok := c.seedWithRetry(ctx, "b", "task"); ok {
		t.Fatal("expected seedWithRetry to fail with a cancelled ctx")
	}
	if attempts != 1 {
		t.Fatalf("expected exactly 1 seed attempt with pre-cancelled ctx, got %d", attempts)
	}
}

func TestDelegateCreatesWorkerOnLeadBranchWithParent(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, true,
		[]state.Session{{Name: "lead", Project: "proj", Worktree: "/wt/lead"}}, &seeded)

	worker, err := c.Delegate(context.Background(), contracts.DelegateRequest{
		FromSession: "lead", ToAgent: "scripter", Task: "écris le module",
	})
	if err != nil {
		t.Fatalf("delegate: %v", err)
	}
	if worker != "lead-scripter" {
		t.Fatalf("nom worker inattendu: %q", worker)
	}
	if cr.spec.Base != "session/lead" {
		t.Fatalf("worker pas branché sur le tip du lead: %q", cr.spec.Base)
	}
	if cr.spec.Parent != "lead" {
		t.Fatalf("parent non posé sur le worker: %q", cr.spec.Parent)
	}
	if cr.spec.Agent != "scripter" {
		t.Fatalf("agent worker inattendu: %q", cr.spec.Agent)
	}
	if len(seeded) != 1 || seeded[0] != "lead-scripter|écris le module" {
		t.Fatalf("tâche non seedée au worker: %v", seeded)
	}
}

func TestDelegateUnknownAgent(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, nil, true,
		[]state.Session{{Name: "lead", Worktree: "/wt/lead"}}, &seeded)
	if _, err := c.Delegate(context.Background(), contracts.DelegateRequest{
		FromSession: "lead", ToAgent: "inconnu", Task: "x"}); err == nil {
		t.Fatalf("agent inconnu devrait échouer")
	}
	if cr.spec.Name != "" {
		t.Fatalf("aucune session ne devrait être créée")
	}
}

func TestDelegateDirtyLeadRefused(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, false,
		[]state.Session{{Name: "lead", Worktree: "/wt/lead"}}, &seeded)
	if _, err := c.Delegate(context.Background(), contracts.DelegateRequest{
		FromSession: "lead", ToAgent: "scripter", Task: "x"}); err == nil {
		t.Fatalf("lead sale devrait être refusé")
	}
	if cr.spec.Name != "" {
		t.Fatalf("aucune session ne devrait être créée sur lead sale")
	}
}

func TestDelegateUnknownLead(t *testing.T) {
	cr := &fakeCreator{}
	var seeded []string
	c := newTestCoordinator(cr, []string{"scripter"}, true, nil, &seeded)
	if _, err := c.Delegate(context.Background(), contracts.DelegateRequest{
		FromSession: "ghost", ToAgent: "scripter", Task: "x"}); err == nil {
		t.Fatalf("lead inconnu devrait échouer")
	}
}

func TestReportDeliversBranchRefAndSummaryToParent(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "worker", Worktree: "/wt/worker", Parent: "lead"},
		}, &seeded)

	parent, err := c.Report(context.Background(), contracts.ReportRequest{
		FromSession: "worker", Summary: "module commité, 12 tests verts",
	})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if parent != "lead" {
		t.Fatalf("parent inattendu: %q", parent)
	}
	if len(seeded) != 1 {
		t.Fatalf("livraison attendue une fois, got %v", seeded)
	}
	if !strings.HasPrefix(seeded[0], "lead|") {
		t.Fatalf("message livré à la mauvaise session: %q", seeded[0])
	}
	if !strings.Contains(seeded[0], "session/worker") || !strings.Contains(seeded[0], "12 tests verts") {
		t.Fatalf("message de livraison incomplet: %q", seeded[0])
	}
}

func TestReportWorkerWithoutParentErrors(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{{Name: "orphan", Worktree: "/wt/orphan"}}, &seeded)
	if _, err := c.Report(context.Background(), contracts.ReportRequest{
		FromSession: "orphan", Summary: "x"}); err == nil {
		t.Fatalf("worker sans parent devrait échouer")
	}
	if len(seeded) != 0 {
		t.Fatalf("rien ne devrait être livré: %v", seeded)
	}
}

func TestReportUnknownWorkerErrors(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true, nil, &seeded)
	if _, err := c.Report(context.Background(), contracts.ReportRequest{
		FromSession: "ghost", Summary: "x"}); err == nil {
		t.Fatalf("worker inconnu devrait échouer")
	}
}

func TestReportParentGoneErrors(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{{Name: "worker", Worktree: "/wt/worker", Parent: "lead"}}, &seeded)
	if _, err := c.Report(context.Background(), contracts.ReportRequest{
		FromSession: "worker", Summary: "x"}); err == nil {
		t.Fatalf("parent disparu devrait échouer")
	}
	if len(seeded) != 0 {
		t.Fatalf("rien ne devrait être livré si le parent a disparu: %v", seeded)
	}
}

func TestReportDirtyWorkerRefused(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, false, // worker sale
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "worker", Worktree: "/wt/worker", Parent: "lead"},
		}, &seeded)
	if _, err := c.Report(context.Background(), contracts.ReportRequest{
		FromSession: "worker", Summary: "x"}); err == nil {
		t.Fatalf("worker non commité devrait être refusé")
	}
	if len(seeded) != 0 {
		t.Fatalf("rien ne devrait être livré depuis un worker sale: %v", seeded)
	}
}

func TestReportCountsSiblingProgress(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w1", Worktree: "/wt/w1", Parent: "lead"},
			{Name: "w2", Worktree: "/wt/w2", Parent: "lead"},
			{Name: "w3", Worktree: "/wt/w3", Parent: "lead"},
		}, &seeded)

	for _, w := range []string{"w1", "w2", "w3"} {
		if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: w, Summary: "ok"}); err != nil {
			t.Fatalf("report %s: %v", w, err)
		}
	}
	if len(seeded) != 3 {
		t.Fatalf("3 livraisons attendues: %v", seeded)
	}
	if !strings.Contains(seeded[0], "(1/3)") {
		t.Fatalf("premier compte faux: %q", seeded[0])
	}
	if !strings.Contains(seeded[1], "(2/3)") {
		t.Fatalf("deuxième compte faux: %q", seeded[1])
	}
	if !strings.Contains(seeded[2], "(3/3)") || !strings.Contains(seeded[2], "tous les workers ont livré") {
		t.Fatalf("dernier compte/suffixe faux: %q", seeded[2])
	}
}

func TestReportAllDoneSuffixOnlyOnLast(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w1", Worktree: "/wt/w1", Parent: "lead"},
			{Name: "w2", Worktree: "/wt/w2", Parent: "lead"},
		}, &seeded)

	if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w1", Summary: "ok"}); err != nil {
		t.Fatalf("report w1: %v", err)
	}
	if strings.Contains(seeded[0], "tous les workers ont livré") {
		t.Fatalf("suffixe prématuré au 1er report: %q", seeded[0])
	}
	if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w2", Summary: "ok"}); err != nil {
		t.Fatalf("report w2: %v", err)
	}
	if !strings.Contains(seeded[1], "tous les workers ont livré") {
		t.Fatalf("suffixe absent au dernier report: %q", seeded[1])
	}
}

func TestReportDoubleReportIdempotent(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w1", Worktree: "/wt/w1", Parent: "lead"},
			{Name: "w2", Worktree: "/wt/w2", Parent: "lead"},
		}, &seeded)

	for i := 0; i < 2; i++ {
		if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w1", Summary: "ok"}); err != nil {
			t.Fatalf("report w1 #%d: %v", i, err)
		}
	}
	if !strings.Contains(seeded[1], "(1/2)") {
		t.Fatalf("double report devrait rester (1/2): %q", seeded[1])
	}
	if strings.Contains(seeded[1], "tous les workers ont livré") {
		t.Fatalf("double report ne doit pas déclencher tous-livrés: %q", seeded[1])
	}
}

func leadWorkerSessions() []state.Session {
	return []state.Session{
		{Name: "lead", Worktree: "/wt/lead"},
		{Name: "w", Parent: "lead", Worktree: "/wt/w"},
	}
}

func TestMergeDeliversWorkerBranchToLead(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true, leadWorkerSessions(), &seeded)
	c.wt.(*fakeWTC).merge = worktree.MergeApplied

	lead, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if lead != "lead" {
		t.Fatalf("lead = %q, want lead", lead)
	}
	if got := c.wt.(*fakeWTC).mergedBranch; got != "session/w" {
		t.Fatalf("merged branch = %q, want session/w", got)
	}
	if len(seeded) != 1 || !strings.Contains(seeded[0], "lead|") || !strings.Contains(seeded[0], "w") {
		t.Fatalf("seeded = %v, want one message to lead mentioning w", seeded)
	}
}

func TestMergeConflictAbortsAndReports(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true, leadWorkerSessions(), &seeded)
	f := c.wt.(*fakeWTC)
	f.merge = worktree.MergeConflict
	f.mergeConf = []string{"a.go", "b.go"}

	lead, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w"})
	if err != nil {
		t.Fatalf("conflict must not be an error: %v", err)
	}
	if lead != "lead" {
		t.Fatalf("lead = %q, want lead", lead)
	}
	if len(seeded) != 1 || !strings.Contains(seeded[0], "conflit") || !strings.Contains(seeded[0], "a.go") {
		t.Fatalf("seeded = %v, want conflict diagnostic listing files", seeded)
	}
}

func TestMergeUpToDateIsNeutral(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true, leadWorkerSessions(), &seeded)
	c.wt.(*fakeWTC).merge = worktree.MergeUpToDate

	if _, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w"}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(seeded) != 1 || !strings.Contains(seeded[0], "déjà à jour") {
		t.Fatalf("seeded = %v, want an up-to-date message", seeded)
	}
}

func TestMergeRefusesNonChildWorker(t *testing.T) {
	var seeded []string
	sessions := []state.Session{
		{Name: "lead", Worktree: "/wt/lead"},
		{Name: "w", Parent: "other", Worktree: "/wt/w"}, // not a child of lead
	}
	c := newTestCoordinator(&fakeCreator{}, nil, true, sessions, &seeded)

	if _, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w"}); err == nil {
		t.Fatal("expected refusal for non-child worker")
	}
	if c.wt.(*fakeWTC).mergedBranch != "" {
		t.Fatal("MergeInto must not be called on a non-child worker")
	}
}

func TestMergeRefusesDirtyWorker(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true, leadWorkerSessions(), &seeded)
	// Lead clean, worker dirty.
	c.wt.(*fakeWTC).cleanBy = map[string]bool{"/wt/lead": true, "/wt/w": false}

	if _, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w"}); err == nil {
		t.Fatal("expected refusal for dirty worker")
	}
	if c.wt.(*fakeWTC).mergedBranch != "" {
		t.Fatal("MergeInto must not be called when worker is dirty")
	}
}

func TestMergeRefusesDirtyLead(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true, leadWorkerSessions(), &seeded)
	// Worker clean, lead dirty.
	c.wt.(*fakeWTC).cleanBy = map[string]bool{"/wt/lead": false, "/wt/w": true}

	if _, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w"}); err == nil {
		t.Fatal("expected refusal for dirty lead")
	}
	if c.wt.(*fakeWTC).mergedBranch != "" {
		t.Fatal("MergeInto must not be called when lead is dirty")
	}
}

func TestMergeUnknownWorker(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{{Name: "lead", Worktree: "/wt/lead"}}, &seeded)
	if _, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "ghost"}); err == nil {
		t.Fatal("expected refusal for unknown worker")
	}
}

func TestMergeUnknownLead(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{{Name: "w", Parent: "lead", Worktree: "/wt/w"}}, &seeded)
	if _, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w"}); err == nil {
		t.Fatal("expected refusal for unknown lead")
	}
}

func TestForgetPurgesLeadCohort(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w1", Worktree: "/wt/w1", Parent: "lead"},
		}, &seeded)

	if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w1", Summary: "ok"}); err != nil {
		t.Fatalf("report w1: %v", err)
	}
	if len(c.reported["lead"]) == 0 {
		t.Fatalf("cohorte du lead devrait être peuplée avant purge")
	}
	c.forget("lead")
	if c.reported["lead"] != nil {
		t.Fatalf("forget(lead) devrait jeter la cohorte, got %v", c.reported["lead"])
	}
}

func TestForgetRemovesWorkerKeepsCountConsistent(t *testing.T) {
	var seeded []string
	c := newTestCoordinator(&fakeCreator{}, nil, true,
		[]state.Session{
			{Name: "lead", Worktree: "/wt/lead"},
			{Name: "w1", Worktree: "/wt/w1", Parent: "lead"},
			{Name: "w2", Worktree: "/wt/w2", Parent: "lead"},
		}, &seeded)

	if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w1", Summary: "ok"}); err != nil {
		t.Fatalf("report w1: %v", err)
	}
	c.forget("w1") // w1 se ferme après avoir livré
	if c.reported["lead"]["w1"] {
		t.Fatalf("forget(w1) devrait retirer w1 des livrés")
	}
	// w2 livre ensuite : done ne doit PAS compter le w1 périmé.
	if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w2", Summary: "ok"}); err != nil {
		t.Fatalf("report w2: %v", err)
	}
	if !strings.Contains(seeded[1], "(1/2)") {
		t.Fatalf("w1 périmé ne doit pas gonfler done: %q", seeded[1])
	}
	if strings.Contains(seeded[1], "tous les workers ont livré") {
		t.Fatalf("pas de faux tous-livrés après purge: %q", seeded[1])
	}
}
