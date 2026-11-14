package host

import (
	"io"
	"testing"
	"testing/synctest"
	"time"
)

func TestStuckStopCannotHoldTheLifecycleBeyondTheGrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		grace := time.Second
		stuck := make(chan struct{})
		defer close(stuck)
		name := "stuck-stop"
		done := make(chan struct{})
		h := &hub{warnings: io.Discard, teardownGrace: grace, live: map[string]liveSession{
			name: {cancel: func() {}, done: done, release: func() {}},
		}}
		h.stopBridge = func(string) error {
			<-stuck
			return nil
		}

		retired := make(chan bool, 1)
		go func() { retired <- h.retireFailed(name, done) }()
		synctest.Wait()

		time.Sleep(2 * grace)
		synctest.Wait()
		select {
		case ok := <-retired:
			if !ok {
				t.Fatal("retireFailed did not retire the live session")
			}
		default:
			t.Fatal("retireFailed still holds the lifecycle after the grace: a stuck Stop froze the hub")
		}
		if !h.lifecycle.TryLock() {
			t.Fatal("the lifecycle stays locked after retireFailed returned")
		}
		h.lifecycle.Unlock()
	})
}
