package host

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestAcceptFailureAfterStopDoesNothing(t *testing.T) {
	var out lockedBuffer
	name := "late-" + newTurnID()
	h := newInflightHub(t, name, time.Second, &out)
	var halted atomic.Int32
	h.stopBridge = func(string) error {
		halted.Add(1)
		return nil
	}
	done := make(chan struct{})
	close(done)
	h.mu.Lock()
	h.live[name] = liveSession{cancel: func() {}, done: done, release: func() {}}
	h.mu.Unlock()

	h.stopLive()
	h.acceptFailed(name, done, errTerminalAccept)

	if n := halted.Load(); n != 0 {
		t.Fatalf("a bridge was halted by an accept failure after stop: %d", n)
	}
	if out.String() != "" {
		t.Fatalf("an accept failure after stop was warned: %q", out.String())
	}
}
