package host

import (
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
		t.Fatalf("stopLive took %v behind a stalled warning writer with a %v grace, want under 1s", elapsed.Round(time.Millisecond), grace)
	}
}

func TestStopWithAFrozenWriterAnnouncesTheHubOverrun(t *testing.T) {
	warnings := newGatedWarnings("stalled-warning")
	t.Cleanup(warnings.open)
	h := newInflightHub(t, "stalled-"+newTurnID(), 5*time.Second, warnings)
	if _, err := h.warnings.Write([]byte("stalled-warning\n")); err != nil {
		t.Fatal(err)
	}

	h.stopLive()
	awaitWarning(t, &warnings.out, hubOverrunText(h, ""))
}

func TestCloseWarningsAnnouncesAnUndrainedOverrun(t *testing.T) {
	warnings := newGatedWarnings("stuck")
	t.Cleanup(warnings.open)
	h := testHub(&hub{warnings: warnings, teardownGrace: 300 * time.Millisecond})
	if _, err := h.warnings.Write([]byte("stuck\n")); err != nil {
		t.Fatal(err)
	}

	h.closeWarnings()
	awaitWarning(t, &warnings.out, hubOverrunText(h, ""))
}
