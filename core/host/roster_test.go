package host

import (
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

func TestRosterProjectsAgents(t *testing.T) {
	root := t.TempDir()
	st := agent.NewStore(root)
	if _, err := st.Create(agent.CreateSpec{Name: "codex", Backend: "codex", Tags: []string{"refactor", "tests"}}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	got := NewRoster(root).Agents()
	if len(got) != 1 {
		t.Fatalf("want 1 agent, got %d (%+v)", len(got), got)
	}
	a := got[0]
	if a.Name != "codex" || a.Backend != "codex" || len(a.Tags) != 2 {
		t.Fatalf("bad projection: %+v", a)
	}
}

func TestRosterEmptyWhenNoRoot(t *testing.T) {
	got := NewRoster(filepath.Join(t.TempDir(), "absent")).Agents()
	if len(got) != 0 {
		t.Fatalf("missing root must yield empty roster, got %+v", got)
	}
}
