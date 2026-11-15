package host

import (
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestDeferredWarningsKeepTheOrderOfTheLockHolders(t *testing.T) {
	var out lockedBuffer
	sink := newWarningSink(&out, englishLostLine)
	h := &hub{warnings: sink, sink: sink, teardownGrace: time.Second}

	h.lifecycle.Lock()
	h.deferWarning(i18n.English, "warn.session_start_recovered", map[string]any{nameParam: "first"})
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
