package host

import (
	"strings"
	"testing"
	"time"
)

func TestStopLiveSharesOneGraceAcrossBlockedSessions(t *testing.T) {
	const sessions = 2
	var warnings lockedBuffer
	h := newInflightHub(t, "stuck-"+newTurnID(), blockedGrace, &warnings)
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	names := []string{"stuck-a", "stuck-b"}
	for _, name := range names {
		h.live[name] = liveSession{cancel: func() {}, done: stuck, release: func() {}}
	}

	started := time.Now()
	h.stopLive()
	bound := 3*blockedGrace + (sessions+3)*warningWriteBudget + blockedStopMargin
	if elapsed := time.Since(started); elapsed > bound {
		t.Fatalf("stopLive held %v for %d blocked sessions, past its bound of %v", elapsed.Round(time.Millisecond), sessions, bound)
	}

	awaitSinkQuiet(t, h.sink)
	out := warnings.String()
	for _, name := range names {
		if occurrences := strings.Count(out, sessionOverrunText(h, name)); occurrences != 1 {
			t.Fatalf("session %s was named in the overrun warnings %d times, want 1:\n%s", name, occurrences, out)
		}
	}
}
