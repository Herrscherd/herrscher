package bridge

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestWithCapabilitiesNamesTheDaemonAndItsVerbs(t *testing.T) {
	got := withCapabilities("MEM", "  gateway — channel post, channel read\n  session — close, list")
	for _, want := range []string{"MEM", "<capabilities>", "herrscher <verb>", "herrscher commands", "channel read", "session — close, list", "</capabilities>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("capabilities block missing %q in:\n%s", want, got)
		}
	}
}

// The summary is worth nothing where it is handed over: it has to reach the
// prompt of every turn, beside the skills and the delegation roster.
func TestTurnPromptCarriesTheCapabilities(t *testing.T) {
	resp := &captureBackend{reply: "ok"}
	runOneTurn(context.Background(), &recordSink{}, resp, nil, contracts.Event{T: "input", Text: "hi"}, nil, nil,
		affordances{caps: "  session — close, list"}, nil)
	if !strings.Contains(resp.contexts[0], "<capabilities>") || !strings.Contains(resp.contexts[0], "session — close, list") {
		t.Fatalf("turn context missing the capabilities block:\n%s", resp.contexts[0])
	}
}

func TestWithCapabilitiesEmptySummaryUnchanged(t *testing.T) {
	for _, summary := range []string{"", "   \n\t"} {
		if got := withCapabilities("MEM", summary); got != "MEM" {
			t.Fatalf("summary %q must return base unchanged, got %q", summary, got)
		}
	}
}
