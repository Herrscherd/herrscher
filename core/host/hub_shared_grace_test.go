package host

import (
	"io"
	"testing"
	"testing/synctest"
	"time"
)

func TestGoDeadSharesOneGraceBetweenHaltAndTeardown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		grace := time.Second
		stuck := make(chan struct{})
		defer close(stuck)
		name := "shared-grace"
		h := &hub{warnings: io.Discard, teardownGrace: grace, live: map[string]liveSession{
			name: {cancel: func() {}, done: make(chan struct{}), release: func() {}},
		}}
		h.stopBridge = func(string) error {
			<-stuck
			return nil
		}

		returned := make(chan struct{})
		start := time.Now()
		go func() {
			h.goDead(name)
			close(returned)
		}()
		<-returned
		if elapsed := time.Since(start); elapsed > grace {
			t.Fatalf("goDead took %v for one dead session, want at most one teardownGrace (%v)", elapsed, grace)
		}
	})
}
