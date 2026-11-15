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
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stopLive took %v behind a stalled warning writer with a %v grace, want under 1s", elapsed.Round(time.Millisecond), grace)
	}
}
