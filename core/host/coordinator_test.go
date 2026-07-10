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
	clean bool
	err   error
}

func (f fakeWTC) IsCleanAt(string) (bool, error) { return f.clean, f.err }
func (f fakeWTC) Branch(name string) string      { return "session/" + name }

type fakeSessions struct{ list []state.Session }

func (f fakeSessions) SnapshotSessions() []state.Session { return f.list }

func newTestCoordinator(cr *fakeCreator, known []string, clean bool, sessions []state.Session, seeded *[]string) *coordinator {
	km := map[string]bool{}
	for _, k := range known {
		km[k] = true
	}
	seed := func(sess, task string) bool { *seeded = append(*seeded, sess+"|"+task); return true }
	return newCoordinator(cr, fakeAgents{known: km}, fakeWTC{clean: clean}, fakeSessions{list: sessions}, seed)
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
