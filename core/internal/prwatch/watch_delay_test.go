package prwatch

import (
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestStableSubscriptionResetsTheFeedBackoff(t *testing.T) {
	clock := newFakeClock()
	feed := &fakeFeed{clock: clock, refuse: errBoom, signals: make(chan contracts.PullRequestSignal)}
	reader := newFakeReader(clock)
	store := &fakeStore{}
	policy := testPolicy(time.Minute)
	policy.Every = 10 * time.Hour
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Feed: feed, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: policy})

	reader.awaitRead(t)
	if first := clock.awaitArm(t); first != time.Second {
		t.Fatalf("wait after a failed subscription = %v, want the first backoff step", first)
	}
	feed.setRefusal(nil)
	clock.advance(time.Second)
	clock.awaitArm(t)
	clock.advance(5 * time.Minute)
	close(feed.signals)
	if again := clock.awaitArm(t); again != time.Second {
		t.Fatalf("wait after a subscription that held five minutes = %v, want the backoff reset to the first step", again)
	}
	stop()
}

func TestDeliveredSignalResetsTheFeedBackoff(t *testing.T) {
	clock := newFakeClock()
	feed := &fakeFeed{clock: clock, refuse: errBoom, signals: make(chan contracts.PullRequestSignal)}
	reader := newFakeReader(clock)
	store := &fakeStore{}
	policy := testPolicy(time.Minute)
	policy.Every = 10 * time.Hour
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Feed: feed, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: policy})

	reader.awaitRead(t)
	clock.awaitArm(t)
	feed.setRefusal(nil)
	clock.advance(time.Second)
	clock.awaitArm(t)
	feed.signals <- contracts.PullRequestSignal{Kind: contracts.PullRequestNewCommit}
	reader.awaitRead(t)
	clock.awaitArm(t)
	close(feed.signals)
	if again := clock.awaitArm(t); again != time.Second {
		t.Fatalf("wait after a subscription that delivered a signal = %v, want the backoff reset to the first step", again)
	}
	stop()
}
