package host

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestTeardownOverrunWarnsOnlyForTheSessionStillStopping(t *testing.T) {
	var warnings bytes.Buffer
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	h := &hub{messages: newHostMessages(), warnings: &warnings, teardownGrace: 300 * time.Millisecond, live: make(map[string]liveSession)}
	h.stopBridge = func(name string) error {
		if name == "blocked" {
			<-stuck
		}
		return nil
	}
	names := []string{"blocked", "healthy-a", "healthy-b"}
	for _, name := range names {
		done := make(chan struct{})
		close(done)
		h.live[name] = liveSession{cancel: func() {}, done: done, release: func() {}}
	}

	window, cancel := h.newGrace()
	defer cancel()
	var retired []retirement
	h.lifecycle.Lock()
	for _, name := range names {
		retired = append(retired, h.goDeadLocked(name, window)...)
	}
	h.lifecycle.Unlock()
	h.awaitRetired(window, retired)

	out := warnings.String()
	if n := strings.Count(out, `"blocked"`); n != 1 {
		t.Fatalf("the blocked session was named %d times in the overrun warnings, want 1:\n%s", n, out)
	}
	for _, name := range []string{"healthy-a", "healthy-b"} {
		if strings.Contains(out, `"`+name+`"`) {
			t.Fatalf("session %s stopped in time but got an overrun warning:\n%s", name, out)
		}
	}
}
