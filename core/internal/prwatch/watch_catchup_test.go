package prwatch

import (
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestResubscriptionReadsWhatTheFeedMissed(t *testing.T) {
	clock := newFakeClock()
	feed := &fakeFeed{clock: clock, signals: make(chan contracts.PullRequestSignal)}
	reader := newFakeReader(clock, reply{pull: open("r1")}, reply{pull: open("r2")})
	store := &fakeStore{}
	policy := testPolicy(time.Minute)
	policy.Every = 10 * time.Hour
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Feed: feed, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: policy})

	reader.awaitRead(t)
	clock.awaitArm(t)
	close(feed.signals)
	pause := clock.awaitArm(t)
	feed.replaceSignals(make(chan contracts.PullRequestSignal))
	clock.advance(pause)
	catchUp := reader.awaitRead(t)
	if catchUp.since != "r1" {
		t.Fatalf("read after the resubscription since = %q, want the revision r1 of the last read", catchUp.since)
	}
	if !catchUp.at.Equal(clock.Now()) {
		t.Fatalf("read after the resubscription at %v, want the moment the feed came back %v", catchUp.at, clock.Now())
	}
	stop()
}
