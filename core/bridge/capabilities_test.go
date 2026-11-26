package bridge

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func TestCapabilitiesBlockNamesTheDaemonAndItsVerbs(t *testing.T) {
	got := capabilitiesBlock("  gateway: channel post, channel read\n  session: close, list")
	for _, want := range []string{"<capabilities>", "herrscher <verb>", "herrscher commands", "channel read", "session: close, list", "</capabilities>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("capabilities block missing %q in:\n%s", want, got)
		}
	}
}

func TestTurnPromptCarriesTheCapabilities(t *testing.T) {
	resp := &captureBackend{reply: "ok"}
	if err := runOneTurn(context.Background(), &recordSink{}, resp, nil, contracts.Event{T: eventwire.EventInput, Text: "hi"}, nil, nil,
		affordances{caps: "  session: close, list"}, nil, 0); err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	if !strings.Contains(resp.contexts[0], "<capabilities>") || !strings.Contains(resp.contexts[0], "session: close, list") {
		t.Fatalf("turn context missing the capabilities block:\n%s", resp.contexts[0])
	}
}

func TestCapabilitiesBlockIsEmptyForAnEmptySummary(t *testing.T) {
	for _, summary := range []string{"", "   \n\t"} {
		if got := capabilitiesBlock(summary); got != "" {
			t.Fatalf("summary %q must write no block, got %q", summary, got)
		}
	}
}
