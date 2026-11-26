package bridge

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type stubRoster struct{ agents []contracts.AgentInfo }

func (s stubRoster) Agents() []contracts.AgentInfo { return s.agents }

func TestDelegationBlockListsAgentsAndMarkers(t *testing.T) {
	r := stubRoster{agents: []contracts.AgentInfo{{Name: "codex", Backend: "codex", Tags: []string{"refactor", "tests"}}}}
	got := delegationBlock(r)
	for _, want := range []string{"<delegation>", "⟢ delegate:", "⟢ route:", "codex", "refactor"} {
		if !strings.Contains(got, want) {
			t.Fatalf("delegation block missing %q in:\n%s", want, got)
		}
	}
}

func TestDelegationBlockIsEmptyWithoutRoster(t *testing.T) {
	if got := delegationBlock(nil); got != "" {
		t.Fatalf("nil roster must write no block, got %q", got)
	}
}

func TestDelegationBlockIsEmptyForAnEmptyRoster(t *testing.T) {
	if got := delegationBlock(stubRoster{}); got != "" {
		t.Fatalf("empty roster must write no block, got %q", got)
	}
}
