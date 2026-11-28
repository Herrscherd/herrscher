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

type rolesRoster struct {
	stubRoster

	roles map[string]map[string]string
}

func (r rolesRoster) WorkerModels(agent string) map[string]string { return r.roles[agent] }

func TestDelegationBlockWithoutRolesIsUnchanged(t *testing.T) {
	agents := []contracts.AgentInfo{{Name: "codex", Backend: "codex", Tags: []string{"refactor"}}}
	plain := delegationBlock(stubRoster{agents: agents})
	withEmptyRoles := delegationBlock(rolesRoster{stubRoster: stubRoster{agents: agents}, roles: map[string]map[string]string{"codex": {}}})
	if plain != withEmptyRoles {
		t.Fatalf("an agent without worker roles must not change the block:\nplain:\n%s\nempty roles:\n%s", plain, withEmptyRoles)
	}
	for _, unwanted := range []string{"roles:", "<agent>/<role>"} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("block without roles mentions %q:\n%s", unwanted, plain)
		}
	}
}

func TestDelegationBlockAnnouncesRolesWithTheirModels(t *testing.T) {
	r := rolesRoster{
		stubRoster: stubRoster{agents: []contracts.AgentInfo{
			{Name: "codex", Backend: "codex", Summary: "writes code"},
			{Name: "scout", Backend: "claude"},
		}},
		roles: map[string]map[string]string{
			"codex": {"summary": "gpt-mini", "research": "claude-haiku-5-5"},
		},
	}
	got := delegationBlock(r)
	for _, want := range []string{
		"⟢ delegate: <agent>/<role>",
		"Use only the roles listed for that agent",
		"  - codex (backend: codex): writes code roles: research=claude-haiku-5-5, summary=gpt-mini\n",
		"  - scout (backend: claude)\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("delegation block missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "scout (backend: claude) roles:") {
		t.Fatalf("agent without roles lists roles:\n%s", got)
	}
}

func TestDelegationBlockRolesNeedARosterThatExposesThem(t *testing.T) {
	r := stubRoster{agents: []contracts.AgentInfo{{Name: "codex", Backend: "codex"}}}
	if got := delegationBlock(r); strings.Contains(got, "<agent>/<role>") {
		t.Fatalf("roster without worker roles announces a role syntax:\n%s", got)
	}
}
