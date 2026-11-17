package host

import (
	"io"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestStuckStopCannotHoldTheLifecycleBeyondTheGrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		grace := time.Second
		stuck := make(chan struct{})
		release := sync.OnceFunc(func() { close(stuck) })
		defer release()
		name := "stuck-stop"
		done := make(chan struct{})
		h := testHub(&hub{warnings: io.Discard, teardownGrace: grace, live: map[string]liveSession{
			name: {cancel: func() {}, done: done, release: func() {}},
		}})
		h.stopBridge = func(string) error {
			<-stuck
			return nil
		}

		retired := make(chan bool, 1)
		go func() {
			_, ok := h.retireFailed(name, done)
			retired <- ok
		}()
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

		release()
		synctest.Wait()
		if err := h.closeAndRemoveSockets(); err != nil {
			t.Fatal(err)
		}
		if !h.errPort.isHalted() {
			t.Fatal("closeAndRemoveSockets returned with the stderr port still open: the hub stopped without halting its stderr goroutine")
		}
	})
}

func TestStuckStopCannotFreezeGoDead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		grace := time.Second
		stuck := make(chan struct{})
		defer close(stuck)
		name := "stuck-dead"
		done := make(chan struct{})
		close(done)
		h := testHub(&hub{warnings: io.Discard, teardownGrace: grace, live: map[string]liveSession{
			name: {cancel: func() {}, done: done, release: func() {}},
		}})
		h.stopBridge = func(string) error {
			<-stuck
			return nil
		}

		returned := make(chan struct{})
		go func() {
			h.goDead(name)
			close(returned)
		}()
		synctest.Wait()

		time.Sleep(2 * grace)
		synctest.Wait()
		select {
		case <-returned:
		default:
			t.Fatal("goDead still holds the hub after the grace: a stuck Stop froze every Dispatch that reconciles")
		}
	})
}
