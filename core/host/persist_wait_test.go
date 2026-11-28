package host

import (
	"testing"
	"time"
)

func awaitPersistWriteInFlight(t *testing.T, h *hub) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for h.persists.group.pending() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the write never entered the persist group: its caller is not wired to the write tracker")
		}
		time.Sleep(time.Millisecond)
	}
}

func persistOverrunText(h *hub) string {
	return newHostMessages().text(h.textLanguage()(), "warn.persist_overrun", map[string]any{graceParam: persistWriteGrace.String()})
}
