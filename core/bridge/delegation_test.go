package bridge

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type stubRoster struct{ agents []contracts.AgentInfo }

func (s stubRoster) Agents() []contracts.AgentInfo { return s.agents }

func TestWithDelegationListsAgentsAndMarkers(t *testing.T) {
	r := stubRoster{agents: []contracts.AgentInfo{{Name: "codex", Backend: "codex", Tags: []string{"refactor", "tests"}}}}
	got := withDelegation("MEM", r)
	for _, want := range []string{"MEM", "<delegation>", "⟢ delegate:", "⟢ route:", "codex", "refactor"} {
		if !strings.Contains(got, want) {
			t.Fatalf("delegation block missing %q in:\n%s", want, got)
		}
	}
}

func TestWithDelegationNilRosterUnchanged(t *testing.T) {
	if got := withDelegation("MEM", nil); got != "MEM" {
		t.Fatalf("nil roster must return base unchanged, got %q", got)
	}
}

func TestWithDelegationEmptyRosterUnchanged(t *testing.T) {
	if got := withDelegation("MEM", stubRoster{}); got != "MEM" {
		t.Fatalf("empty roster must return base unchanged, got %q", got)
	}
}
