package host

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
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
	awaitHubLogs(t, h)
	unconfirmed := unconfirmedAtClose(t, logs.String())
	if lost < 1 || unconfirmed != lost {
		t.Fatalf("the hub overrun behind a frozen writer was counted lost=%d but the journal at Close reports unconfirmed=%d, want each loss announced", lost, unconfirmed)
	}
	warnings.open()
	awaitSinkQuiet(t, h.sink)
	if written := strings.Contains(warnings.out.String(), hubOverrunText(h, "")); written && lost > 0 {
		t.Fatalf("the hub overrun was written and counted as lost=%d", lost)
	}
}

func awaitSinkQuiet(t *testing.T, sink *warningSink) {
	t.Helper()
	waitUntil(t, "the warning sink to finish its last write", func() bool {
		return ask(sink, func() bool { return sink.inflight == nil && len(sink.queue) == 0 })
	})
}

func TestCloseWarningsLosesOrLogsAnUndrainedOverrun(t *testing.T) {
	logger, logs := newCapturedLogger(nil)
	warnings := newGatedWarnings("stuck")
	t.Cleanup(warnings.open)
	h := testHubLogged(t, &hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), warnings: warnings, teardownGrace: 300 * time.Millisecond}, logger)
	if _, err := h.warnings.Write([]byte("stuck\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}

	h.closeWarnings()
	lost := droppedWarnings(h.sink)
	awaitHubLogs(t, h)
	unconfirmed := unconfirmedAtClose(t, logs.String())
	if lost < 1 || unconfirmed != lost {
		t.Fatalf("the undrained overrun behind a frozen writer was counted lost=%d but the journal at Close reports unconfirmed=%d, want each loss announced", lost, unconfirmed)
	}
	warnings.open()
	awaitSinkQuiet(t, h.sink)
	if written := strings.Contains(warnings.out.String(), hubOverrunText(h, "")); written && lost > 0 {
		t.Fatalf("the undrained overrun was written and counted as lost=%d", lost)
	}
}
