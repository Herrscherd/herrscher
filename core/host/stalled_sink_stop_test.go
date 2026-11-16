package host

import (
	"strings"
	"testing"
	"time"
)

func TestStopLiveIsBoundedWhenTheWarningWriterStalls(t *testing.T) {
	const grace = 5 * time.Second
	warnings := newGatedWarnings("stalled-warning")
	t.Cleanup(warnings.open)
	h := newInflightHub(t, "stalled-"+newTurnID(), grace, warnings)
	if _, err := h.warnings.Write([]byte("stalled-warning\n")); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	h.stopLive()
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("stopLive took %v behind a stalled warning writer with a %v grace, want under 2s", elapsed.Round(time.Millisecond), grace)
	}
}

func TestStopWithAFrozenWriterCountsTheHubOverrunAsLost(t *testing.T) {
	warnings := newGatedWarnings("stalled-warning")
	t.Cleanup(warnings.open)
	h := newInflightHub(t, "stalled-"+newTurnID(), 5*time.Second, warnings)
	if _, err := h.warnings.Write([]byte("stalled-warning\n")); err != nil {
		t.Fatal(err)
	}

	h.stopLive()
	if got := h.sink.droppedCount(); got < 1 {
		t.Fatalf("the hub overrun behind a writer frozen for the whole budget was not counted as lost, dropped = %d", got)
	}
	if out := warnings.out.String(); strings.Contains(out, hubOverrunText(h, "")) {
		t.Fatalf("the hub overrun reached a writer that stayed frozen: %q", out)
	}
}

func TestCloseWarningsCountsAnUndrainedOverrunAsLost(t *testing.T) {
	warnings := newGatedWarnings("stuck")
	t.Cleanup(warnings.open)
	h := testHub(&hub{warnings: warnings, teardownGrace: 300 * time.Millisecond})
	if _, err := h.warnings.Write([]byte("stuck\n")); err != nil {
		t.Fatal(err)
	}

	h.closeWarnings()
	if got := h.sink.droppedCount(); got < 1 {
		t.Fatalf("the undrained overrun behind a frozen writer was not counted as lost, dropped = %d", got)
	}
}
