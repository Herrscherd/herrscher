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

func TestStopWithAFrozenWriterNeverBothWritesAndCountsTheHubOverrun(t *testing.T) {
	warnings := newGatedWarnings("stalled-warning")
	t.Cleanup(warnings.open)
	h := newInflightHub(t, "stalled-"+newTurnID(), 5*time.Second, warnings)
	if _, err := h.warnings.Write([]byte("stalled-warning\n")); err != nil {
		t.Fatal(err)
	}

	h.stopLive()
	warnings.open()
	time.Sleep(200 * time.Millisecond)
	written := strings.Contains(warnings.out.String(), hubOverrunText(h, ""))
	counted := h.sink.droppedCount() > 0
	if written == counted {
		t.Fatalf("the hub overrun behind a frozen writer was written=%v and counted as lost=%v, want exactly one of the two", written, counted)
	}
}

func TestCloseWarningsNeverBothWritesAndCountsAnUndrainedOverrun(t *testing.T) {
	warnings := newGatedWarnings("stuck")
	t.Cleanup(warnings.open)
	h := testHub(&hub{warnings: warnings, teardownGrace: 300 * time.Millisecond})
	if _, err := h.warnings.Write([]byte("stuck\n")); err != nil {
		t.Fatal(err)
	}

	h.closeWarnings()
	warnings.open()
	time.Sleep(200 * time.Millisecond)
	written := strings.Contains(warnings.out.String(), hubOverrunText(h, ""))
	counted := h.sink.droppedCount() > 0
	if written == counted {
		t.Fatalf("the undrained overrun behind a frozen writer was written=%v and counted as lost=%v, want exactly one of the two", written, counted)
	}
}
