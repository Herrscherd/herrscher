package prwatch

import (
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestCancelDuringRefusalPauseStopsWithoutSubscribing(t *testing.T) {
	clock := newFakeClock()
	feed := &fakeFeed{clock: clock, refuse: contracts.ErrFeedRefused, signals: make(chan contracts.PullRequestSignal)}
	reader := newFakeReader(clock)
	store := &fakeStore{}
	policy := testPolicy(time.Minute)
	policy.Every = 2 * time.Hour
	stop, finished := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Feed: feed, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: policy})

	reader.awaitRead(t)
	if pause := clock.awaitArm(t); pause != time.Hour {
		t.Fatalf("wait during the refusal pause = %v, want the pause of 1h", pause)
	}
	stop()
	awaitStop(t, finished)
	if got := len(feed.subscriptions()); got != 1 {
		t.Fatalf("subscriptions = %d after a cancel in the pause, want the single refused attempt", got)
	}
}
