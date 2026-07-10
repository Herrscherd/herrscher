package host

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
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
}

func (f fakeWTC) IsCleanAt(string) (bool, error) { return f.clean, f.err }
func (f fakeWTC) Branch(name string) string      { return "session/" + name }
func (f fakeWTC) BranchExistsAt(_, branch string) (bool, error) {
	return f.branches[branch], nil
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
	return newCoordinator(cr, fakeAgents{known: km}, fakeWTC{clean: clean, branches: branches}, fakeSessions{list: sessions}, closer, seed)
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
	c := newCoordinator(cr, fakeAgents{known: km}, fakeWTC{clean: true}, fakeSessions{list: []state.Session{
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
