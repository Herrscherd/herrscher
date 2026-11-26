package prwatch

import (
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestClosedFeedIsSubscribedAgainWithoutAnError(t *testing.T) {
	clock := newFakeClock()
	feed := &fakeFeed{clock: clock, signals: make(chan contracts.PullRequestSignal)}
	reader := newFakeReader(clock, reply{pull: open("r1")})
	store := &fakeStore{}
	policy := testPolicy(time.Minute)
	policy.Every = 10 * time.Hour
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Feed: feed, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: policy})

	reader.awaitRead(t)
	clock.awaitArm(t)
	close(feed.signals)
	pause := clock.awaitArm(t)
	if pause != time.Second {
		t.Fatalf("pause after a closed feed = %v, want the first backoff step", pause)
	}
	feed.replaceSignals(make(chan contracts.PullRequestSignal))
	clock.advance(pause)
	clock.awaitArm(t)
	if got := len(feed.subscriptions()); got != 2 {
		t.Fatalf("subscriptions = %d after the closed feed and its pause, want it asked again", got)
	}
	stop()
}
