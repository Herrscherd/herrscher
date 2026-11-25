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

// Policy sets how often a watch reads the forge and how it backs off.
type Policy struct {
	Every     time.Duration
	Backoff   obs.Backoff
	Silence   time.Duration
	FeedPause time.Duration
}

// Watch is the following of one pull request. Since is the revision of the last read saved,
// so the first read asks the forge for a change only; zero asks for the whole request. Stagger
// delays the first read. Save persists a read whose state, draft flag, URL or revision differs
// from the last one saved; a failed Save counts as a failed read and is retried in full. A nil
// Limit gives the watch a limit of its own.
type Watch struct {
	Ref     contracts.PullRequestRef
	Since   contracts.Revision
	Stagger time.Duration
	Reader  Reader
	Feed    Feed
	Save    func(ctx context.Context, pull contracts.PullRequest) error
	Limit   *Limit
	Clock   Clock
	Policy  Policy
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
	if w.Limit == nil {
		w.Limit = &Limit{}
	}
	r := &runner{watch: w, since: w.Since, failures: w.Policy.Backoff, feedFailures: w.Policy.Backoff}
	r.loop(ctx, w.Stagger)
}

type runner struct {
	watch        Watch
	since        contracts.Revision
	saved        *recordKey
	failures     obs.Backoff
	feedFailures obs.Backoff
	signals      <-chan FeedEvent
	heardAt      time.Time
	subscribeAt  time.Time
}

type recordKey struct {
	state    contracts.PullRequestState
	draft    bool
	url      string
	revision contracts.Revision
}

func (r *runner) loop(ctx context.Context, due time.Duration) {
	for {
		if !r.idle(ctx, r.heldFor(due)) {
			return
		}
		wait, ended := r.read(ctx)
		if ended {
			return
		}
		due = wait
	}
}

func (r *runner) heldFor(due time.Duration) time.Duration {
	return max(due, r.watch.Limit.Until().Sub(r.watch.Clock.Now()))
}

func (r *runner) idle(ctx context.Context, due time.Duration) bool {
	r.subscribe(ctx)
	if due <= 0 {
		return ctx.Err() == nil
	}
	timer := r.watch.Clock.After(due)
	silence := r.silenceTimer()
	select {
	case <-ctx.Done():
		return false
	case <-timer:
		return true
	case <-silence:
		r.signals = nil
		r.subscribeAt = r.watch.Clock.Now()
		return true
	case _, open := <-r.signals:
		if !open {
			r.signals = nil
			r.subscribeAt = r.watch.Clock.Now().Add(r.feedFailures.Next(0))
			return true
		}
		r.heardAt = r.watch.Clock.Now()
		return true
	}
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
	signals, err := r.watch.Feed.Subscribe(ctx, r.watch.Ref)
	if err != nil {
		r.subscribeAt = now.Add(r.feedDelay(err))
		return
	}
	r.signals, r.heardAt = signals, now
	r.feedFailures = r.watch.Policy.Backoff
}

func (r *runner) feedDelay(err error) time.Duration {
	if errors.Is(err, ErrFeedRefused) {
		return r.watch.Policy.FeedPause
	}
	return r.feedFailures.Next(0)
}

func (r *runner) read(ctx context.Context) (time.Duration, bool) {
	pull, err := r.watch.Reader.PullRequest(ctx, r.watch.Ref, r.since)
	if ctx.Err() != nil {
		return 0, true
	}
	if errors.Is(err, contracts.ErrForgeNotModified) {
		r.failures = r.watch.Policy.Backoff
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
	key := recordKey{state: pull.State, draft: pull.Draft, url: pull.URL, revision: pull.Revision}
	if r.saved != nil && *r.saved == key {
		return nil
	}
	if err := r.watch.Save(ctx, pull); err != nil {
		return err
	}
	r.saved = &key
	return nil
}
