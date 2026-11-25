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
	stop, finished := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Feed: feed, Save: store.save, Limit: limit, Clock: clock, Policy: testPolicy(time.Minute)})

	reader.awaitRead(t)
	clock.awaitArm(t)
	limit.Hold(clock.Now().Add(10 * time.Minute))
	feed.signals <- contracts.PullRequestSignal{Kind: contracts.PullRequestNewCommit}
	stop()
	awaitStop(t, finished)
	if got := len(reader.readCalls()); got != 1 {
		t.Fatalf("reads = %d after a signal during a hold, want the first read only: the signal waits for the reset", got)
	}
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

func (f *fakeFeed) replaceSignals(signals chan contracts.PullRequestSignal) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signals = signals
}

func (f *fakeFeed) setRefusal(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refuse = err
}

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
