package host

import (
	"context"
	"testing"
	"time"
)

func TestClaimIdleRefusesASessionWithATurnOrAQueuedMessage(t *testing.T) {
	sessions := newSessionTable()
	driver := newSessionDriver("s1", nil, nil, nil)
	sessions.register("s1", driver)

	release, ok := sessions.claimIdle("s1")
	if !ok {
		t.Fatal("an idle session must be claimable")
	}
	if _, again := sessions.claimIdle("s1"); again {
		t.Fatal("a claimed session must refuse a second claim")
	}
	release()

	if !driver.enterTurn(t.Context()) {
		t.Fatal("entering a turn on an idle session must succeed")
	}
	if _, ok := sessions.claimIdle("s1"); ok {
		t.Fatal("a session with a turn in flight must not be claimable")
	}
	driver.releaseTurn()

	driver.queue <- queued{}
	if _, ok := sessions.claimIdle("s1"); ok {
		t.Fatal("a session with a queued message must not be claimable")
	}
	<-driver.queue

	if _, ok := sessions.claimIdle("absente"); !ok {
		t.Fatal("a session without a running driver has nothing to race with")
	}
}

func TestPumpExitsOnTeardownWhileAClaimIsHeld(t *testing.T) {
	sessions := newSessionTable()
	driver := newSessionDriver("s1", nil, nil, nil)
	sessions.register("s1", driver)
	release, ok := sessions.claimIdle("s1")
	if !ok {
		t.Fatal("an idle session must be claimable")
	}
	defer release()
	pumpCtx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	driver.queue <- queued{}
	go func() {
		driver.pump(pumpCtx)
		close(done)
	}()
	stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a pump blocked by a rewind claim must still exit on teardown")
	}
}
