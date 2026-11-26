package prwatch

import (
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestSignalDuringAHoldDoesNotReadBeforeTheReset(t *testing.T) {
	clock := newFakeClock()
	feed := &fakeFeed{clock: clock, signals: make(chan contracts.PullRequestSignal)}
	reader := newFakeReader(clock, reply{pull: open("r1")}, reply{pull: open("r2")})
	store := &fakeStore{}
	limit := &Limit{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Feed: feed, Save: store.save, Limit: limit, Clock: clock, Policy: testPolicy(time.Minute)})

	reader.awaitRead(t)
	clock.awaitArm(t)
	reset := clock.Now().Add(10 * time.Minute)
	limit.Hold(reset)
	feed.signals <- contracts.PullRequestSignal{Kind: contracts.PullRequestNewCommit}
	clock.awaitArm(t)
	if got := len(reader.readCalls()); got != 1 {
		t.Fatalf("reads = %d after a signal during a hold, want the first read only: the signal waits for the reset", got)
	}
	clock.advance(10 * time.Minute)
	if second := reader.awaitRead(t); !second.at.Equal(reset) {
		t.Fatalf("read after a signal during a hold at %v, want exactly the reset %v", second.at, reset)
	}
	stop()
}

func TestSignalsDuringAHoldReadOnceAtTheReset(t *testing.T) {
	clock := newFakeClock()
	feed := &fakeFeed{clock: clock, signals: make(chan contracts.PullRequestSignal)}
	reader := newFakeReader(clock, reply{pull: open("r1")}, reply{pull: open("r2")})
	store := &fakeStore{}
	limit := &Limit{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Feed: feed, Save: store.save, Limit: limit, Clock: clock, Policy: testPolicy(time.Minute)})

	reader.awaitRead(t)
	clock.awaitArm(t)
	reset := clock.Now().Add(10 * time.Minute)
	limit.Hold(reset)
	for range 3 {
		feed.signals <- contracts.PullRequestSignal{Kind: contracts.PullRequestNewCommit}
		clock.awaitArm(t)
	}
	clock.advance(10 * time.Minute)
	if second := reader.awaitRead(t); !second.at.Equal(reset) {
		t.Fatalf("read after three signals during a hold at %v, want exactly the reset %v", second.at, reset)
	}
	clock.awaitArm(t)
	if got := len(reader.readCalls()); got != 2 {
		t.Fatalf("reads = %d after three signals during a hold, want the first read and one read at the reset", got)
	}
	stop()
}
