package host

import (
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestDeferredWarningsKeepTheOrderOfTheLockHolders(t *testing.T) {
	var out lockedBuffer
	h := testHub(&hub{warnings: &out, teardownGrace: time.Second})

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
	awaitWarning(t, &out, "second")
	<-second

	got := out.String()
	if first, last := strings.Index(got, "first"), strings.Index(got, "second"); first < 0 || first > last {
		t.Fatalf("the warning deferred under the lock came out after a later one:\n%s", got)
	}
}
