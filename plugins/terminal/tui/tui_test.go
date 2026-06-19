package tui

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// A reply carrying a turn cost must surface it in the TUI transcript, mirroring
// the host progress summary for non-EventSink gateways.
func TestRenderEventShowsCost(t *testing.T) {
	m := &model{}
	m.renderEvent(contracts.Event{T: "reply", Text: "done", Done: true, Cost: 0.0042})

	joined := strings.Join(m.lines, "\n")
	if !strings.Contains(joined, "done") {
		t.Fatalf("reply text dropped: %q", joined)
	}
	if !strings.Contains(joined, "$0.0042") {
		t.Fatalf("cost dropped from transcript: %q", joined)
	}
}

// A free turn (no cost) must not append a bogus $0.00 line.
func TestRenderEventOmitsZeroCost(t *testing.T) {
	m := &model{}
	m.renderEvent(contracts.Event{T: "reply", Text: "done", Done: true})

	for _, l := range m.lines {
		if strings.Contains(l, "$") {
			t.Fatalf("zero-cost turn must not show a cost line; got %q", l)
		}
	}
}
