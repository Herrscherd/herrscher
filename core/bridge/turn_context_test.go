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

func TestTurnContextSizesEveryBlockWithZeroForAnEmptyOne(t *testing.T) {
	aff := affordances{caps: "  session: list"}
	got := composeTurnContext("MEM", nil, aff).sizes()
	want := []contracts.InjectedPart{
		{Part: contracts.ContextMemory, Tokens: contextwindow.EstimateTokens("MEM")},
		{Part: contracts.ContextSkills, Tokens: 0},
		{Part: contracts.ContextDelegation, Tokens: 0},
		{Part: contracts.ContextCapabilities, Tokens: contextwindow.EstimateTokens(capabilitiesBlock(aff.caps))},
		{Part: contracts.ContextIdentity, Tokens: 0},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("sizes = %+v, want %+v", got, want)
	}
}

func TestTurnContextSizesTheSelfToolsDeclarationsWithoutInjectingThem(t *testing.T) {
	turn := composeTurnContext("MEM", nil, affordances{selfTools: 9214})
	sizes := turn.sizes()
	last := sizes[len(sizes)-1]
	if last.Part != contracts.ContextSelfTools || last.Tokens != contextwindow.EstimateBytes(9214) {
		t.Fatalf("sizes = %+v, want the self tools part last at %d tokens", sizes, contextwindow.EstimateBytes(9214))
	}
	if turn.String() != "MEM" {
		t.Fatalf("turn context = %q, want the declarations kept out of the prompt", turn.String())
	}
}

func TestTurnReplyWithNothingInjectedStillCarriesZeroSizes(t *testing.T) {
	sink := &recordSink{}
	if err := runOneTurn(context.Background(), sink, &captureBackend{reply: "ok"}, stubMemory{}, contracts.Event{T: eventwire.EventInput, Text: "hi"}, nil, nil,
		affordances{}, nil, 0); err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	reply := sink.events[len(sink.events)-1]
	if !reply.Done || len(reply.Injected) != 5 {
		t.Fatalf("an empty turn must still say it measured every block: %+v", reply.Injected)
	}
	for _, part := range reply.Injected {
		if part.Tokens != 0 {
			t.Fatalf("empty turn sized %s at %d", part.Part, part.Tokens)
		}
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
	want := composeTurnContext(recall, nil, affordances{}).sizes()
	if want[0].Tokens != contextwindow.EstimateTokens(recall) || !reply.Done || !slices.Equal(reply.Injected, want) {
		t.Fatalf("reply injected = %+v (done %v), want %+v", reply.Injected, reply.Done, want)
	}
}

func nothingInjected() []contracts.InjectedPart {
	return composeTurnContext("", nil, affordances{}).sizes()
}
