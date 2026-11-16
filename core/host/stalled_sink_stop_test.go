package host

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStopLiveIsBoundedWhenTheWarningWriterStalls(t *testing.T) {
	const grace = 5 * time.Second
	warnings := newGatedWarnings("stalled-warning")
	t.Cleanup(warnings.open)
	h := newInflightHub(t, "stalled-"+newTurnID(), grace, warnings)
	if _, err := h.warnings.Write([]byte("stalled-warning\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}

	started := time.Now()
	h.stopLive()
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("stopLive took %v behind a stalled warning writer with a %v grace, want under 2s", elapsed.Round(time.Millisecond), grace)
	}
}

func TestStopWithAFrozenWriterLosesOrLogsTheHubOverrun(t *testing.T) {
	logs := captureDefaultLog(t)
	warnings := newGatedWarnings("stalled-warning")
	t.Cleanup(warnings.open)
	h := newInflightHub(t, "stalled-"+newTurnID(), 5*time.Second, warnings)
	if _, err := h.warnings.Write([]byte("stalled-warning\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}

	h.stopLive()
	h.closeWarnings()
	lost := droppedWarnings(h.sink)
	unconfirmed := unconfirmedAtClose(t, logs.String())
	if lost == 0 && unconfirmed == 0 {
		t.Fatal("the hub overrun behind a frozen writer was neither counted lost nor reported unconfirmed at Close")
	}
	warnings.open()
	time.Sleep(200 * time.Millisecond)
	if written := strings.Contains(warnings.out.String(), hubOverrunText(h, "")); written && lost > 0 {
		t.Fatalf("the hub overrun was written and counted as lost=%d", lost)
	}
}

func TestCloseWarningsLosesOrLogsAnUndrainedOverrun(t *testing.T) {
	logs := captureDefaultLog(t)
	warnings := newGatedWarnings("stuck")
	t.Cleanup(warnings.open)
	h := testHub(&hub{warnings: warnings, teardownGrace: 300 * time.Millisecond})
	if _, err := h.warnings.Write([]byte("stuck\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}

	h.closeWarnings()
	lost := droppedWarnings(h.sink)
	unconfirmed := unconfirmedAtClose(t, logs.String())
	if lost == 0 && unconfirmed == 0 {
		t.Fatal("the undrained overrun behind a frozen writer was neither counted lost nor reported unconfirmed at Close")
	}
	warnings.open()
	time.Sleep(200 * time.Millisecond)
	if written := strings.Contains(warnings.out.String(), hubOverrunText(h, "")); written && lost > 0 {
		t.Fatalf("the undrained overrun was written and counted as lost=%d", lost)
	}
}
