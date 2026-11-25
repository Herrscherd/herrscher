package host

import (
	"context"
	"log/slog"
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
	prWatchSilence   = 10 * time.Minute
	prWatchFeedPause = 30 * time.Minute
	prWatchRetryBase = 30 * time.Second
	prWatchRetryCap  = 15 * time.Minute
	prWatchJitter    = 0.2
	prWatchFactor    = 2
)

type prWatcher struct {
	prs     sessionPRs
	store   *state.State
	clock   prwatch.Clock
	limit   *prwatch.Limit
	policy  prwatch.Policy
	mu      sync.Mutex
	watches map[string]*watchEntry
}

type watchEntry struct {
	number int
	cancel context.CancelFunc
}

func newSessionPRWatcher(messages *clitext.Messages, lang languageSource, store *state.State, opener forgeOpener, credentials contracts.ForgeCredentials) *prWatcher {
	return &prWatcher{
		prs:     newSessionPRs(messages, lang, store, opener, credentials, sessionPRForgeLimit),
		store:   store,
		clock:   prwatch.SystemClock(),
		limit:   &prwatch.Limit{},
		policy:  prWatchPolicy(),
		watches: make(map[string]*watchEntry),
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

func (w *prWatcher) Run(ctx context.Context) {
	var watches sync.WaitGroup
	defer watches.Wait()
	ticker := time.NewTicker(prWatchScan)
	defer ticker.Stop()
	for {
		w.scan(ctx, &watches)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *prWatcher) scan(ctx context.Context, watches *sync.WaitGroup) {
	wanted := make(map[string]int)
	sessions := w.store.SnapshotSessions()
	for index := range sessions {
		sess := &sessions[index]
		if sess.PullRequest == 0 || prwatch.Ended(sess.PullRequestState) {
			continue
		}
		wanted[sess.Name] = sess.PullRequest
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for name, entry := range w.watches {
		if number, ok := wanted[name]; !ok || number != entry.number {
			entry.cancel()
			delete(w.watches, name)
		}
	}
	for name, number := range wanted {
		if _, running := w.watches[name]; !running {
			w.start(ctx, watches, name, number)
		}
	}
}

func (w *prWatcher) start(ctx context.Context, watches *sync.WaitGroup, name string, number int) {
	watchCtx, cancel := context.WithCancel(ctx)
	entry := &watchEntry{number: number, cancel: cancel}
	w.watches[name] = entry
	watches.Add(1)
	go func() {
		defer watches.Done()
		defer w.forget(name, entry)
		w.follow(watchCtx, name)
	}()
}

func (w *prWatcher) forget(name string, entry *watchEntry) {
	entry.cancel()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.watches[name] == entry {
		delete(w.watches, name)
	}
}

func (w *prWatcher) follow(ctx context.Context, name string) {
	target, err := w.prs.target(ctx, name)
	if err != nil {
		slog.Debug("session pull request not followed", "session", name, "err", err)
		return
	}
	prwatch.Watch{
		Ref:    target.ref,
		Reader: target.forge,
		Save:   w.save(name, target.ref.Number()),
		Limit:  w.limit,
		Clock:  w.clock,
		Policy: w.policy,
	}.Run(ctx)
}

func (w *prWatcher) save(name string, number int) func(context.Context, contracts.PullRequest) error {
	return func(ctx context.Context, pull contracts.PullRequest) error {
		release, err := w.prs.refreshes.acquire(ctx, name)
		if err != nil {
			return err
		}
		defer release()
		record := state.PullRequestRecord{Number: number, State: pull.State, Draft: pull.Draft, URL: pull.URL}
		if err := w.store.RefreshPullRequest(name, record); err != nil {
			slog.Warn("session pull request state not saved", "session", name, "err", err)
			return err
		}
		return nil
	}
}
