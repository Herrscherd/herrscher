package host

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

func TestEnsureCodexAgentCreatesWhenAbsent(t *testing.T) {
	st := agent.NewStore(t.TempDir())
	ensureCodexAgent(st)
	a, ok := st.Get("codex")
	if !ok {
		t.Fatal("codex agent was not created")
	}
	if a.Backend != "codex" || len(a.Tags) == 0 {
		t.Fatalf("codex agent malformed: %+v", a)
	}
}

func TestEnsureCodexAgentIdempotent(t *testing.T) {
	st := agent.NewStore(t.TempDir())
	if _, err := st.Create(agent.CreateSpec{Name: "codex", Backend: "codex", Tags: []string{"custom"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ensureCodexAgent(st)
	a, _ := st.Get("codex")
	if len(a.Tags) != 1 || a.Tags[0] != "custom" {
		t.Fatalf("ensure clobbered an existing agent: %+v", a)
	}
}
