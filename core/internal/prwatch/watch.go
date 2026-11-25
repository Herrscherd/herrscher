// Package prwatch follows the pull request of a session. It reads the request on a cadence,
// asks again with the revision of its last read so an unchanged request costs nothing,
// waits out the forge rate limit, and stops when the request is merged or closed. Where the
// forge grants an event feed, a signal from it triggers a read at once, and a silent or
// refused feed is subscribed again later.
package prwatch

import (
	"context"
	"errors"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/obs"
)

// Reader reads a pull request from its forge. A read with a non-empty since returns
// [contracts.ErrForgeNotModified] when the request has not changed since that revision.
type Reader interface {
	PullRequest(ctx context.Context, ref contracts.PullRequestRef, since contracts.Revision) (contracts.PullRequest, error)
}

// Policy sets how often a watch reads the forge and how it backs off. Silence is how long an
// open feed may stay quiet before it is cut and subscribed again, and FeedPause is how long a
// refused feed is left alone.
type Policy struct {
	Every     time.Duration
	Backoff   obs.Backoff
	Silence   time.Duration
	FeedPause time.Duration
}

// Watch is the following of one pull request. Since is the revision of the last read saved,
// so the first read asks the forge for a change only; zero asks for the whole request. Stagger
// delays the first read. Feed is the forge's event feed, nil when the forge grants none. Save
// persists a read whose state, draft flag, URL, head commit, merge state, checks or latest
// review differs from the last one saved; a failed Save counts as a failed read and is retried
// in full. Limit is shared with every watch on the same forge, and NewWatch refuses a nil one.
type Watch struct {
	Ref     contracts.PullRequestRef
	Since   contracts.Revision
	Stagger time.Duration
	Reader  Reader
	Feed    contracts.PullRequestFeed
	Save    func(ctx context.Context, pull contracts.PullRequest) error
	Limit   *Limit
	Clock   Clock
	Policy  Policy
}

// ErrLimitRequired reports a watch built without the limit shared with the other watches of
// its forge.
var ErrLimitRequired = errors.New("prwatch: watch without a shared limit")

// NewWatch returns watch once it can run, and ErrLimitRequired when it has no Limit.
func NewWatch(watch Watch) (Watch, error) {
	if watch.Limit == nil {
		return Watch{}, ErrLimitRequired
	}
	return watch, nil
}

// Ended reports whether a pull request in state is merged or closed, so that no further
// change is expected and the watch stops.
func Ended(state contracts.PullRequestState) bool {
	return state == contracts.PullRequestClosed || state == contracts.PullRequestMerged
}

// Run follows the pull request until ctx is done or the request has ended. It returns only
// after its loop has stopped, so a caller that waits for Run to return owns no goroutine of
// the watch.
func (w Watch) Run(ctx context.Context) {
	follower := &runner{watch: w, since: w.Since, failures: w.Policy.Backoff, feedFailures: w.Policy.Backoff}
	follower.loop(ctx, w.Stagger)
}

type runner struct {
	watch        Watch
	since        contracts.Revision
	saved        *recordKey
	failures     obs.Backoff
	feedFailures obs.Backoff
	signals      <-chan contracts.PullRequestSignal
	cancelFeed   context.CancelFunc
	heardAt      time.Time
	subscribeAt  time.Time
}

type recordKey struct {
	state        contracts.PullRequestState
	draft        bool
	url          string
	revision     contracts.Revision
	headSHA      contracts.CommitSHA
	merge        contracts.MergeState
	checks       contracts.ChecksState
	latestReview contracts.ReviewID
}

func (r *runner) loop(ctx context.Context, due time.Duration) {
	defer r.closeFeed()
	for {
		if !r.idle(ctx, due) {
			return
		}
		wait, ended := r.read(ctx)
		if ended {
			return
		}
		due = wait
	}
}

func (r *runner) idle(ctx context.Context, due time.Duration) bool {
	deadline := r.watch.Clock.Now().Add(due)
	for {
		r.subscribe(ctx)
		now := r.watch.Clock.Now()
		remaining := r.readTime(deadline).Sub(now)
		if remaining <= 0 {
			return ctx.Err() == nil
		}
		timer := r.watch.Clock.After(r.nextWake(now, remaining))
		silence := r.silenceTimer()
		select {
		case <-ctx.Done():
			return false
		case <-timer:
		case <-silence:
			r.pauseFeed(r.feedFailures.Next(0))
		case signal, open := <-r.signals:
			if !open {
				r.pauseFeed(r.feedFailures.Next(0))
				break
			}
			if signal.Err != nil {
				r.pauseFeed(r.feedDelay(signal.Err))
				break
			}
			r.heardAt = r.watch.Clock.Now()
			r.feedFailures = r.watch.Policy.Backoff
			deadline = r.watch.Clock.Now()
		}
	}
}

func (r *runner) readTime(deadline time.Time) time.Time {
	if until := r.watch.Limit.Until(); until.After(deadline) {
		return until
	}
	return deadline
}

func (r *runner) nextWake(now time.Time, remaining time.Duration) time.Duration {
	if r.watch.Feed != nil && r.signals == nil && r.subscribeAt.After(now) {
		return min(remaining, r.subscribeAt.Sub(now))
	}
	return remaining
}

func (r *runner) silenceTimer() <-chan time.Time {
	if r.signals == nil {
		return nil
	}
	return r.watch.Clock.After(r.heardAt.Add(r.watch.Policy.Silence).Sub(r.watch.Clock.Now()))
}

func (r *runner) subscribe(ctx context.Context) {
	if r.watch.Feed == nil || r.signals != nil {
		return
	}
	now := r.watch.Clock.Now()
	if now.Before(r.subscribeAt) {
		return
	}
	feedCtx, cancel := context.WithCancel(ctx)
	signals, err := r.watch.Feed.PullRequestEvents(feedCtx, r.watch.Ref)
	if err != nil {
		cancel()
		r.subscribeAt = now.Add(r.feedDelay(err))
		return
	}
	r.signals, r.cancelFeed, r.heardAt = signals, cancel, now
}

func (r *runner) feedDelay(err error) time.Duration {
	if errors.Is(err, contracts.ErrFeedRefused) {
		return r.watch.Policy.FeedPause
	}
	return r.feedFailures.Next(0)
}

func (r *runner) pauseFeed(delay time.Duration) {
	r.closeFeed()
	r.subscribeAt = r.watch.Clock.Now().Add(delay)
}

func (r *runner) closeFeed() {
	if r.cancelFeed != nil {
		r.cancelFeed()
	}
	r.cancelFeed, r.signals = nil, nil
}

func (r *runner) read(ctx context.Context) (time.Duration, bool) {
	pull, err := r.watch.Reader.PullRequest(ctx, r.watch.Ref, r.since)
	if ctx.Err() != nil {
		return 0, true
	}
	if errors.Is(err, contracts.ErrForgeNotModified) {
		r.failures = r.watch.Policy.Backoff
		if r.saved == nil && r.since != "" {
			r.since = ""
			return 0, false
		}
		return r.watch.Policy.Every, false
	}
	if err == nil {
		return r.observe(ctx, pull)
	}
	if limited, ok := errors.AsType[*contracts.ForgeRateLimitedError](err); ok && !limited.Reset.IsZero() {
		return r.hold(limited.Reset), false
	}
	return r.failures.Next(0), false
}

func (r *runner) hold(reset time.Time) time.Duration {
	now := r.watch.Clock.Now()
	if !reset.After(now) {
		reset = now.Add(r.failures.Next(0))
	}
	r.watch.Limit.Hold(reset)
	return reset.Sub(now)
}

func (r *runner) observe(ctx context.Context, pull contracts.PullRequest) (time.Duration, bool) {
	if err := r.record(ctx, pull); err != nil {
		r.since = ""
		return r.failures.Next(0), false
	}
	r.since = pull.Revision
	r.failures = r.watch.Policy.Backoff
	if Ended(pull.State) {
		return 0, true
	}
	return r.watch.Policy.Every, false
}

func (r *runner) record(ctx context.Context, pull contracts.PullRequest) error {
	key := keyOf(pull)
	if r.saved != nil && *r.saved == key {
		return nil
	}
	if err := r.watch.Save(ctx, pull); err != nil {
		return err
	}
	r.saved = &key
	return nil
}

func keyOf(pull contracts.PullRequest) recordKey {
	return recordKey{
		state:        pull.State,
		draft:        pull.Draft,
		url:          pull.URL,
		revision:     pull.Revision,
		headSHA:      pull.HeadSHA,
		merge:        pull.Merge,
		checks:       pull.Checks,
		latestReview: pull.LatestReview,
	}
}
