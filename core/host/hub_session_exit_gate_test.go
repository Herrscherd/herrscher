package host

import (
	"runtime"
	"testing"
	"time"
)

const sessionExitHold = 500 * time.Millisecond

func holdSessionExit(t *testing.T, sessions *sessionTable, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for sessions.lookup(name) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("session %s never registered its driver", name)
		}
		runtime.Gosched()
	}
	sessions.mu.Lock()
	time.AfterFunc(sessionExitHold, sessions.mu.Unlock)
}
