package host

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/prwatch"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	prWatchScan      = time.Minute
	prWatchEvery     = time.Minute
	prWatchSilence   = 45 * time.Second
	prWatchFeedPause = time.Hour
	prWatchRetryBase = 30 * time.Second
	prWatchRetryCap  = 15 * time.Minute
	prWatchJitter    = 0.2
	prWatchFactor    = 2
)

type prWatcher struct {
	prs      sessionPRs
	store    *state.State
	clock    prwatch.Clock
	limit    *prwatch.Limit
	policy   prwatch.Policy
	stagger  func() time.Duration
	watches  sync.WaitGroup
	mu       sync.Mutex
	follows  map[string]*sessionFollow
	groups   map[prGroupKey]*prGroup
	refusals map[string]*prRefusal
}

type prGroupKey struct {
	repo   string
	number int
}

type sessionFollow struct {
	number int
	cancel context.CancelFunc
	done   chan struct{}
}

type prGroup struct {
	key      prGroupKey
	saveMu   sync.Mutex
	last     contracts.PullRequest
	hasLast  bool
	members  map[string]bool
	cancel   context.CancelFunc
	finished chan struct{}
}

type prRefusal struct {
	number int
	until  time.Time
	retry  obs.Backoff
}

func newSessionPRWatcher(messages *clitext.Messages, lang languageSource, store *state.State, opener forgeOpener, credentials contracts.ForgeCredentials) *prWatcher {
	return &prWatcher{
		prs:      newSessionPRs(messages, lang, store, opener, credentials, sessionPRForgeLimit),
		store:    store,
		clock:    prwatch.SystemClock(),
		limit:    &prwatch.Limit{},
		policy:   prWatchPolicy(),
		stagger:  prWatchStagger,
		follows:  make(map[string]*sessionFollow),
		groups:   make(map[prGroupKey]*prGroup),
		refusals: make(map[string]*prRefusal),
	}
}

func prWatchPolicy() prwatch.Policy {
	return prwatch.Policy{
		Every:     prWatchEvery,
		Backoff:   obs.Backoff{Base: prWatchRetryBase, Max: prWatchRetryCap, Factor: prWatchFactor, Jitter: prWatchJitter},
		Silence:   prWatchSilence,
		FeedPause: prWatchFeedPause,
	}
}

func prWatchStagger() time.Duration {
	jitter := rand.Float64
	return time.Duration(jitter() * float64(prWatchEvery))
}

func (w *prWatcher) Run(ctx context.Context) {
	defer w.watches.Wait()
	ticker := time.NewTicker(prWatchScan)
	defer ticker.Stop()
	for {
		w.scan(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *prWatcher) scan(ctx context.Context) {
	wanted := w.wanted()
	w.mu.Lock()
	defer w.mu.Unlock()
	retired := make(map[string]<-chan struct{})
	for name, follow := range w.follows {
		if number, ok := wanted[name]; !ok || number != follow.number {
			follow.cancel()
			delete(w.follows, name)
			retired[name] = follow.done
		}
	}
	for name := range w.refusals {
		if _, ok := wanted[name]; !ok {
			delete(w.refusals, name)
		}
	}
	for name, number := range wanted {
		if _, running := w.follows[name]; running || w.refusing(name, number) {
			continue
		}
		w.startLocked(ctx, name, number, retired[name])
	}
}

func (w *prWatcher) wanted() map[string]int {
	wanted := make(map[string]int)
	sessions := w.store.SnapshotSessions()
	for index := range sessions {
		sess := &sessions[index]
		if sess.PullRequest == 0 || prwatch.Ended(sess.PullRequestState) {
			continue
		}
		wanted[sess.Name] = sess.PullRequest
	}
	return wanted
}

func (w *prWatcher) refusing(name string, number int) bool {
	refusal, ok := w.refusals[name]
	return ok && refusal.number == number && w.clock.Now().Before(refusal.until)
}

func (w *prWatcher) startLocked(ctx context.Context, name string, number int, previous <-chan struct{}) {
	followCtx, cancel := context.WithCancel(ctx)
	follow := &sessionFollow{number: number, cancel: cancel, done: make(chan struct{})}
	w.follows[name] = follow
	w.watches.Add(1)
	go func() {
		defer w.watches.Done()
		defer close(follow.done)
		defer w.forget(name, follow)
		w.follow(followCtx, name, number, previous)
	}()
}

func (w *prWatcher) forget(name string, follow *sessionFollow) {
	follow.cancel()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.follows[name] == follow {
		delete(w.follows, name)
	}
}

func (w *prWatcher) follow(ctx context.Context, name string, number int, previous <-chan struct{}) {
	if !awaitPrevious(ctx, previous) {
		return
	}
	target, err := w.prs.target(ctx, name)
	if err != nil {
		if ctx.Err() == nil {
			slog.Debug("session pull request not followed", "session", name, "err", err)
			w.refuse(name, number)
		}
		return
	}
	key := prGroupKey{repo: target.ref.Repo().String(), number: number}
	group, err := w.join(ctx, key, name, target)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("session pull request not followed", "session", name, "err", err)
			w.refuse(name, number)
		}
		return
	}
	w.catchUp(ctx, group, name)
	select {
	case <-ctx.Done():
	case <-group.finished:
	}
	w.leave(group, name)
}

func awaitPrevious(ctx context.Context, previous <-chan struct{}) bool {
	if previous == nil {
		return ctx.Err() == nil
	}
	select {
	case <-previous:
		return ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

func (w *prWatcher) refuse(name string, number int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	refusal, ok := w.refusals[name]
	if !ok || refusal.number != number {
		refusal = &prRefusal{number: number, retry: obs.Backoff{Base: prWatchScan, Max: prWatchRetryCap, Factor: prWatchFactor}}
		w.refusals[name] = refusal
	}
	refusal.until = w.clock.Now().Add(refusal.retry.Next(0))
}

func (w *prWatcher) join(ctx context.Context, key prGroupKey, name string, target prTarget) (*prGroup, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.refusals, name)
	group, ok := w.groups[key]
	if !ok {
		started, err := w.startGroupLocked(ctx, key, name, target)
		if err != nil {
			return nil, err
		}
		group = started
	}
	group.members[name] = true
	return group, nil
}

func feedOf(forge contracts.Forge) contracts.PullRequestFeed {
	feed, ok := forge.(contracts.PullRequestFeed)
	if !ok {
		return nil
	}
	return feed
}

func (w *prWatcher) startGroupLocked(ctx context.Context, key prGroupKey, name string, target prTarget) (*prGroup, error) {
	groupCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	group := &prGroup{key: key, members: make(map[string]bool), cancel: cancel, finished: make(chan struct{})}
	watch, err := prwatch.NewWatch(prwatch.Watch{
		Ref:     target.ref,
		Since:   w.recordedRevision(name),
		Stagger: w.stagger(),
		Reader:  target.forge,
		Feed:    feedOf(target.forge),
		Save:    w.save(group),
		Limit:   w.limit,
		Clock:   w.clock,
		Policy:  w.policy,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	w.groups[key] = group
	w.watches.Add(1)
	go func() {
		defer w.watches.Done()
		defer w.finish(group)
		watch.Run(groupCtx)
	}()
	return group, nil
}

func (w *prWatcher) recordedRevision(name string) contracts.Revision {
	sess, ok := w.store.FindSession(name)
	if !ok {
		return ""
	}
	return sess.PullRequestRevision
}

func (w *prWatcher) finish(group *prGroup) {
	group.cancel()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.groups[group.key] == group {
		delete(w.groups, group.key)
	}
	close(group.finished)
}

func (w *prWatcher) leave(group *prGroup, name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(group.members, name)
	if len(group.members) == 0 && w.groups[group.key] == group {
		group.cancel()
		delete(w.groups, group.key)
	}
}

func (w *prWatcher) catchUp(ctx context.Context, group *prGroup, name string) {
	group.saveMu.Lock()
	defer group.saveMu.Unlock()
	if !group.hasLast {
		return
	}
	if err := w.saveMember(ctx, name, group.key.number, group.last); err != nil && ctx.Err() == nil {
		slog.Warn("session pull request catch-up not saved", "session", name, "err", err)
	}
}

func (w *prWatcher) save(group *prGroup) func(context.Context, contracts.PullRequest) error {
	return func(ctx context.Context, pull contracts.PullRequest) error {
		group.saveMu.Lock()
		defer group.saveMu.Unlock()
		group.last, group.hasLast = pull, true
		var first error
		for _, name := range w.members(group) {
			if err := w.saveMember(ctx, name, group.key.number, pull); err != nil && first == nil {
				first = err
			}
		}
		return first
	}
}

func (w *prWatcher) members(group *prGroup) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	names := make([]string, 0, len(group.members))
	for name := range group.members {
		names = append(names, name)
	}
	return names
}

func (w *prWatcher) saveMember(ctx context.Context, name string, number int, pull contracts.PullRequest) error {
	release, err := w.prs.refreshes.acquire(ctx, name)
	if err != nil {
		return err
	}
	defer release()
	record := state.NewPullRequestRecord(number, pull)
	if err := w.store.RefreshPullRequest(name, record); err != nil {
		slog.Warn("session pull request state not saved", "session", name, "err", err)
		return err
	}
	return nil
}
