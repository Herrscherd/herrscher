package prwatch

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/obs"
)

var (
	errBoom       = errors.New("forge unreachable")
	errSaveFailed = errors.New("state not saved")
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []fakeTimer
	armed  chan time.Duration
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC), armed: make(chan time.Duration, 128)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.timers = append(c.timers, fakeTimer{at: c.now.Add(d), ch: ch})
	c.armed <- d
	return ch
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	pending := c.timers[:0]
	for _, timer := range c.timers {
		if timer.at.After(c.now) {
			pending = append(pending, timer)
			continue
		}
		timer.ch <- c.now
	}
	c.timers = pending
}

func (c *fakeClock) awaitArm(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-c.armed:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("the watch armed no wait")
		return 0
	}
}

func (c *fakeClock) tick(t *testing.T) time.Duration {
	t.Helper()
	d := c.awaitArm(t)
	c.advance(d)
	return d
}

type readCall struct {
	since contracts.Revision
	at    time.Time
}

type reply struct {
	pull contracts.PullRequest
	err  error
}

type fakeReader struct {
	clock   *fakeClock
	started chan readCall
	mu      sync.Mutex
	replies []reply
	calls   []readCall
}

func newFakeReader(clock *fakeClock, replies ...reply) *fakeReader {
	return &fakeReader{clock: clock, started: make(chan readCall, 128), replies: replies}
}

func (r *fakeReader) PullRequest(_ context.Context, _ contracts.PullRequestRef, since contracts.Revision) (contracts.PullRequest, error) {
	call := readCall{since: since, at: r.clock.Now()}
	r.mu.Lock()
	r.calls = append(r.calls, call)
	next := reply{err: contracts.ErrForgeNotModified}
	if len(r.replies) > 0 {
		next = r.replies[0]
		r.replies = r.replies[1:]
	}
	r.mu.Unlock()
	r.started <- call
	return next.pull, next.err
}

func (r *fakeReader) awaitRead(t *testing.T) readCall {
	t.Helper()
	select {
	case call := <-r.started:
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("the watch made no read")
		return readCall{}
	}
}

func (r *fakeReader) readCalls() []readCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]readCall(nil), r.calls...)
}

type fakeStore struct {
	mu       sync.Mutex
	failures int
	saved    []contracts.PullRequest
}

func (s *fakeStore) save(_ context.Context, pull contracts.PullRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failures > 0 {
		s.failures--
		return errSaveFailed
	}
	s.saved = append(s.saved, pull)
	return nil
}

func (s *fakeStore) persisted() []contracts.PullRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]contracts.PullRequest(nil), s.saved...)
}

type fakeFeed struct {
	clock      *fakeClock
	refuse     error
	signals    chan contracts.PullRequestSignal
	mu         sync.Mutex
	subscribed []time.Time
	contexts   []context.Context
}

func (f *fakeFeed) PullRequestEvents(ctx context.Context, _ contracts.PullRequestRef) (<-chan contracts.PullRequestSignal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subscribed = append(f.subscribed, f.clock.Now())
	f.contexts = append(f.contexts, ctx)
	if f.refuse != nil {
		return nil, f.refuse
	}
	return f.signals, nil
}

func (f *fakeFeed) subscriptions() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Time(nil), f.subscribed...)
}

func (f *fakeFeed) subscriptionContext(index int) context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.contexts[index]
}

func testRef(t *testing.T) contracts.PullRequestRef {
	t.Helper()
	repo, err := contracts.ParseRepoRef("https://github.com/acme/app")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := contracts.NewPullRequestRef(repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func testPolicy(limit time.Duration) Policy {
	return Policy{
		Every:     time.Minute,
		Backoff:   obs.Backoff{Base: time.Second, Max: limit, Factor: 2},
		FeedPause: time.Hour,
	}
}

func startWatch(t *testing.T, watch Watch) (context.CancelFunc, <-chan struct{}) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		watch.Run(ctx)
	}()
	t.Cleanup(stop)
	return stop, finished
}

func awaitStop(t *testing.T, finished <-chan struct{}) {
	t.Helper()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the watch did not stop")
	}
}

func open(revision contracts.Revision) contracts.PullRequest {
	return contracts.PullRequest{Revision: revision, State: contracts.PullRequestOpen, URL: "u"}
}

func TestNotModifiedReadChangesNothing(t *testing.T) {
	clock := newFakeClock()
	reader := newFakeReader(clock, reply{pull: open("r1")}, reply{err: contracts.ErrForgeNotModified}, reply{err: contracts.ErrForgeNotModified})
	store := &fakeStore{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)})

	reader.awaitRead(t)
	if d := clock.tick(t); d != time.Minute {
		t.Fatalf("wait after the first read = %v, want Every", d)
	}
	if second := reader.awaitRead(t); second.since != "r1" {
		t.Fatalf("second read since = %q, want the revision of the first", second.since)
	}
	if d := clock.tick(t); d != time.Minute {
		t.Fatalf("wait after a 304 = %v, want Every and no backoff", d)
	}
	if third := reader.awaitRead(t); third.since != "r1" {
		t.Fatalf("third read since = %q, want the revision still held after a 304", third.since)
	}
	clock.awaitArm(t)
	if got := len(store.persisted()); got != 1 {
		t.Fatalf("persisted = %d, want 1: a 304 changes nothing", got)
	}
	stop()
}

func TestChangedStateIsPersistedAndAClosedRequestEndsTheWatch(t *testing.T) {
	clock := newFakeClock()
	draft := open("r2")
	draft.Draft = true
	closed := contracts.PullRequest{Revision: "r3", State: contracts.PullRequestClosed, URL: "u"}
	reader := newFakeReader(clock, reply{pull: open("r1")}, reply{pull: draft}, reply{pull: closed})
	store := &fakeStore{}
	_, finished := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)})

	clock.tick(t)
	clock.tick(t)
	awaitStop(t, finished)

	saved := store.persisted()
	if len(saved) != 3 {
		t.Fatalf("persisted = %d, want 3 (open, draft, closed)", len(saved))
	}
	if !saved[1].Draft || saved[2].State != contracts.PullRequestClosed {
		t.Fatalf("persisted states = %+v, want the draft then the closed request", saved)
	}
}

func TestFailedSaveIsRetriedWithAFullRead(t *testing.T) {
	clock := newFakeClock()
	reader := newFakeReader(clock, reply{pull: open("r1")}, reply{pull: open("r1")})
	store := &fakeStore{failures: 1}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Since: "r0", Reader: reader, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)})

	if first := reader.awaitRead(t); first.since != "r0" {
		t.Fatalf("first read since = %q, want the persisted revision r0", first.since)
	}
	if d := clock.tick(t); d != time.Second {
		t.Fatalf("wait after a failed save = %v, want the first backoff step", d)
	}
	retry := reader.awaitRead(t)
	if retry.since != "" {
		t.Fatalf("retry read since = %q, want an unconditional read", retry.since)
	}
	clock.awaitArm(t)
	if got := len(store.persisted()); got != 1 {
		t.Fatalf("persisted = %d, want the retried read saved", got)
	}
	stop()
}

func TestFailedReadsBackOffGrowingAndCapped(t *testing.T) {
	clock := newFakeClock()
	reader := newFakeReader(clock, reply{err: errBoom}, reply{err: errBoom}, reply{err: errBoom}, reply{err: errBoom})
	store := &fakeStore{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(4 * time.Second)})

	var waits []time.Duration
	for range 4 {
		waits = append(waits, clock.tick(t))
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second}
	for index := range want {
		if waits[index] != want[index] {
			t.Fatalf("waits = %v, want %v", waits, want)
		}
	}
	stop()
}

func TestRateLimitHoldsEveryReadUntilTheReset(t *testing.T) {
	clock := newFakeClock()
	reset := clock.Now().Add(10 * time.Minute)
	limit := &Limit{}
	reader := newFakeReader(clock, reply{err: &contracts.ForgeRateLimitedError{Reset: reset}}, reply{pull: open("r1")})
	store := &fakeStore{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Save: store.save, Limit: limit, Clock: clock, Policy: testPolicy(time.Minute)})

	reader.awaitRead(t)
	if d := clock.tick(t); d != 10*time.Minute {
		t.Fatalf("wait = %v, want the time until the reset", d)
	}
	second := reader.awaitRead(t)
	if !second.at.Equal(reset) {
		t.Fatalf("second read at %v, want exactly the reset %v", second.at, reset)
	}
	clock.awaitArm(t)
	if !limit.Until().Equal(reset) {
		t.Fatalf("limit holds until %v, want %v", limit.Until(), reset)
	}
	stop()
}

func TestSharedLimitDelaysAWatchThatHasNoError(t *testing.T) {
	clock := newFakeClock()
	reset := clock.Now().Add(5 * time.Minute)
	limit := &Limit{}
	limit.Hold(reset)
	reader := newFakeReader(clock, reply{pull: open("r1")})
	store := &fakeStore{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Save: store.save, Limit: limit, Clock: clock, Policy: testPolicy(time.Minute)})

	if d := clock.tick(t); d != 5*time.Minute {
		t.Fatalf("wait = %v, want the time until the shared reset", d)
	}
	first := reader.awaitRead(t)
	if first.at.Before(reset) {
		t.Fatalf("first read at %v, want no read before %v", first.at, reset)
	}
	stop()
}

func TestCancelStopsTheWatchWithoutAnotherRead(t *testing.T) {
	clock := newFakeClock()
	reader := newFakeReader(clock, reply{pull: open("r1")})
	store := &fakeStore{}
	stop, finished := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)})

	reader.awaitRead(t)
	clock.tick(t)
	reader.awaitRead(t)
	stop()
	awaitStop(t, finished)
	if got := len(reader.readCalls()); got != 2 {
		t.Fatalf("reads = %d after cancel, want the reads already started and no more", got)
	}
}

func TestRefusedFeedPausesBeforeSubscribingAgain(t *testing.T) {
	clock := newFakeClock()
	feed := &fakeFeed{clock: clock, refuse: contracts.ErrFeedRefused, signals: make(chan contracts.PullRequestSignal)}
	reader := newFakeReader(clock)
	store := &fakeStore{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Feed: feed, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)})

	for guard := 0; len(feed.subscriptions()) < 2; guard++ {
		if guard > 100 {
			t.Fatal("the refused feed was never asked again")
		}
		clock.tick(t)
	}
	times := feed.subscriptions()
	if gap := times[1].Sub(times[0]); gap < time.Hour {
		t.Fatalf("feed asked again after %v, want at least the pause of 1h", gap)
	}
	stop()
}

func TestFeedSignalTriggersAReadAtOnce(t *testing.T) {
	clock := newFakeClock()
	feed := &fakeFeed{clock: clock, signals: make(chan contracts.PullRequestSignal)}
	reader := newFakeReader(clock)
	store := &fakeStore{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Feed: feed, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)})

	reader.awaitRead(t)
	clock.awaitArm(t)
	feed.signals <- contracts.PullRequestSignal{Kind: contracts.PullRequestNewCommit}
	reader.awaitRead(t)
	clock.awaitArm(t)
	if got := len(reader.readCalls()); got != 2 {
		t.Fatalf("reads = %d after a signal, want a read at once without advancing the clock", got)
	}
	stop()
}

func TestQuietFeedStaysSubscribed(t *testing.T) {
	clock := newFakeClock()
	feed := &fakeFeed{clock: clock, signals: make(chan contracts.PullRequestSignal)}
	reader := newFakeReader(clock)
	store := &fakeStore{}
	policy := testPolicy(time.Minute)
	policy.Every = 10 * time.Minute
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Feed: feed, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: policy})

	reader.awaitRead(t)
	clock.awaitArm(t)
	clock.advance(time.Minute)
	clock.advance(10 * time.Minute)
	reader.awaitRead(t)
	if err := feed.subscriptionContext(0).Err(); err != nil {
		t.Fatalf("the feed was cut after a quiet minute (%v): the host no longer cuts a quiet feed", err)
	}
	stop()
}

func TestFeedErrCancelsTheSubscriptionAndTriggersNoRead(t *testing.T) {
	clock := newFakeClock()
	feed := &fakeFeed{clock: clock, signals: make(chan contracts.PullRequestSignal)}
	reader := newFakeReader(clock)
	store := &fakeStore{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Feed: feed, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)})

	reader.awaitRead(t)
	clock.awaitArm(t)
	feed.signals <- contracts.PullRequestSignal{Err: errBoom}
	awaitDone(feed.subscriptionContext(0), t)
	clock.awaitArm(t)
	if got := len(reader.readCalls()); got != 1 {
		t.Fatalf("reads = %d after a feed error, want none: the read waits for the cadence", got)
	}
	stop()
}

func TestRefusalMidStreamPausesTheFeed(t *testing.T) {
	clock := newFakeClock()
	feed := &fakeFeed{clock: clock, signals: make(chan contracts.PullRequestSignal)}
	reader := newFakeReader(clock)
	store := &fakeStore{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Feed: feed, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)})

	reader.awaitRead(t)
	clock.awaitArm(t)
	feed.signals <- contracts.PullRequestSignal{Err: contracts.ErrFeedRefused}
	awaitDone(feed.subscriptionContext(0), t)
	for guard := 0; len(feed.subscriptions()) < 2; guard++ {
		if guard > 100 {
			t.Fatal("the refused feed was never asked again")
		}
		clock.tick(t)
	}
	times := feed.subscriptions()
	if gap := times[1].Sub(times[0]); gap < time.Hour {
		t.Fatalf("feed asked again after %v, want at least the pause of 1h", gap)
	}
	stop()
}

func awaitDone(ctx context.Context, t *testing.T) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the feed subscription was not cancelled")
	}
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
