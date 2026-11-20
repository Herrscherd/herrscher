package host

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestDeferredWarningsKeepTheOrderOfTheLockHolders(t *testing.T) {
	gate := newGatedWarnings("blocker")
	t.Cleanup(gate.open)
	h := testHub(t, &hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), warnings: gate, teardownGrace: time.Second})
	if _, err := h.warnings.Write([]byte("blocker\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitGatedWrite(t, gate)

	h.lifecycle.Lock()
	h.deferWarning(i18n.English, "warn.session_start_recovered", map[string]any{nameParam: "first"})
	if queued := queuedWarnings(h.sink); queued != 1 {
		t.Fatalf("the warning deferred under the lock is queued %d times before the lock is released, want 1", queued)
	}
	second := runInBackground(func() {
		h.lifecycle.Lock()
		h.deferWarning(i18n.English, "warn.session_start_recovered", map[string]any{nameParam: "second"})
		h.releaseLifecycle()
	})
	h.releaseLifecycle()
	gate.open()
	awaitWarning(t, &gate.out, "second")
	<-second

	got := gate.out.String()
	if first, last := strings.Index(got, "first"), strings.Index(got, "second"); first < 0 || first > last {
		t.Fatalf("the warning deferred under the lock came out after a later one:\n%s", got)
	}
}

func TestReleaseLifecycleSharesOneBudgetAcrossDeferredWarnings(t *testing.T) {
	const deferred = 20
	gate := newGatedWarnings("frozen")
	t.Cleanup(gate.open)
	h := testHub(t, &hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), warnings: gate, teardownGrace: time.Second})
	if _, err := h.warnings.Write([]byte("frozen\n")); !errors.Is(err, errWarningLate) {
		t.Fatalf("write returned %v, want errWarningLate", err)
	}
	awaitGatedWrite(t, gate)

	h.lifecycle.Lock()
	for range deferred {
		h.deferWarning(i18n.English, "warn.session_start_recovered", map[string]any{nameParam: "stalled"})
	}
	started := time.Now()
	h.releaseLifecycle()
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("releaseLifecycle took %v to release %d deferred warnings behind a frozen writer, want one budget of about %v", elapsed.Round(time.Millisecond), deferred, warningWriteBudget)
	}
}
