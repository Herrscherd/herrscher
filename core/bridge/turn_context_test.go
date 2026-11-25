package bridge

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/contextwindow"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/identity"
	"github.com/Herrscherd/herrscher/core/skills"
)

type stubMemory struct {
	contracts.Orchestrator

	recall string
}

func (m stubMemory) Context(context.Context) string { return m.recall }

func (stubMemory) Observe(context.Context, contracts.Prompt, string) error { return nil }

func TestTurnContextJoinsTheBlocksInTheirOrder(t *testing.T) {
	aff := affordances{caps: "  session: list", user: identity.Identity{Name: "Jane Doe"}}
	got := composeTurnContext("MEM", nil, aff).String()
	want := "MEM" + blockSeparator + capabilitiesBlock(aff.caps) + blockSeparator + identityBlock(aff.user)
	if got != want {
		t.Fatalf("turn context:\n got %q\nwant %q", got, want)
	}
}

func TestTurnContextKeepsEveryBlockInItsOrder(t *testing.T) {
	root := t.TempDir()
	writeBridgeSkill(t, root, "demo", "name: demo\ndescription: a demo\n", "DEMO BODY\n")
	eng := skills.NewEngine([]string{filepath.Join(root, ".claude", "skills")})
	aff := affordances{
		roster: stubRoster{agents: []contracts.AgentInfo{{Name: "codex", Backend: "codex"}}},
		caps:   "  session: list",
		user:   identity.Identity{Name: "Jane Doe"},
	}
	got := composeTurnContext("MEM", eng, aff).String()
	want := strings.Join([]string{"MEM", eng.Menu(), delegationBlock(aff.roster), capabilitiesBlock(aff.caps), identityBlock(aff.user)}, blockSeparator)
	if eng.Menu() == "" || got != want {
		t.Fatalf("turn context:\n got %q\nwant %q", got, want)
	}
}

func TestTurnContextSizesOnlyTheBlocksItWrote(t *testing.T) {
	aff := affordances{caps: "  session: list"}
	got := composeTurnContext("MEM", nil, aff).sizes()
	want := []contracts.InjectedPart{
		{Part: contracts.ContextMemory, Tokens: contextwindow.EstimateTokens("MEM")},
		{Part: contracts.ContextCapabilities, Tokens: contextwindow.EstimateTokens(capabilitiesBlock(aff.caps))},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("sizes = %+v, want %+v", got, want)
	}
}

func TestTurnReplyCarriesTheInjectedSizes(t *testing.T) {
	sink := &recordSink{}
	recall := strings.Repeat("remembered fact ", 40)
	if err := runOneTurn(context.Background(), sink, &captureBackend{reply: "ok"}, stubMemory{recall: recall}, contracts.Event{T: eventwire.EventInput, Text: "hi"}, nil, nil,
		affordances{}, nil, 0); err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	reply := sink.events[len(sink.events)-1]
	want := []contracts.InjectedPart{{Part: contracts.ContextMemory, Tokens: contextwindow.EstimateTokens(recall)}}
	if !reply.Done || !slices.Equal(reply.Injected, want) {
		t.Fatalf("reply injected = %+v (done %v), want %+v", reply.Injected, reply.Done, want)
	}
}
