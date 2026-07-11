package host

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

func TestPickAgentScoresTags(t *testing.T) {
	roster := []agent.Agent{
		{Name: "netter", Tags: []string{"network", "sockets"}},
		{Name: "scripter", Tags: []string{"lua", "roblox"}},
	}
	got, ok := pickAgent(roster, "Implémente le module network avec des sockets")
	if !ok || got != "netter" {
		t.Fatalf("pickAgent = %q,%v want netter,true", got, ok)
	}
}

func TestPickAgentNoMatchRefuses(t *testing.T) {
	roster := []agent.Agent{{Name: "scripter", Tags: []string{"lua"}}}
	if got, ok := pickAgent(roster, "écris de la doc en markdown"); ok {
		t.Fatalf("pickAgent = %q,true want _,false", got)
	}
}

func TestPickAgentTieBreaksByName(t *testing.T) {
	// Both score 1 on "lua"; roster is sorted by name, so the smallest wins.
	roster := []agent.Agent{
		{Name: "alpha", Tags: []string{"lua"}},
		{Name: "beta", Tags: []string{"lua"}},
	}
	if got, _ := pickAgent(roster, "un module lua"); got != "alpha" {
		t.Fatalf("pickAgent tie = %q want alpha", got)
	}
}

func TestPickAgentHighestScoreWins(t *testing.T) {
	roster := []agent.Agent{
		{Name: "alpha", Tags: []string{"lua"}},
		{Name: "beta", Tags: []string{"lua", "roblox"}},
	}
	if got, _ := pickAgent(roster, "module lua pour roblox"); got != "beta" {
		t.Fatalf("pickAgent = %q want beta", got)
	}
}

func TestPickAgentCaseAndPunctuationInsensitive(t *testing.T) {
	roster := []agent.Agent{{Name: "netter", Tags: []string{"http"}}}
	if got, ok := pickAgent(roster, "gère le HTTP, stp."); !ok || got != "netter" {
		t.Fatalf("pickAgent = %q,%v want netter,true", got, ok)
	}
}

func TestPickAgentEmptyRosterRefuses(t *testing.T) {
	if _, ok := pickAgent(nil, "n'importe quoi"); ok {
		t.Fatal("pickAgent(nil) = ok, want !ok")
	}
}

func TestPickAgentIgnoresUntaggedAgents(t *testing.T) {
	roster := []agent.Agent{
		{Name: "bare"},
		{Name: "netter", Tags: []string{"network"}},
	}
	if got, ok := pickAgent(roster, "un peu de network"); !ok || got != "netter" {
		t.Fatalf("pickAgent = %q,%v want netter,true", got, ok)
	}
}
