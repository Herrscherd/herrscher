package host

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/internal/forge"
	"github.com/Herrscherd/herrscher/core/internal/reviewloop"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	envReviewAuthors = "HERRSCHER_REVIEW_AUTHORS"

	reviewStoreAttempts = 5
)

var (
	errReviewPositionBusy = sessionPRError{clitext.SessionPRReplyNotRecorded}
	errReviewNoSession    = sessionPRError{clitext.SessionPRReplyNotRecorded}
)

type sessionFinder interface {
	FindSession(name string) (state.Session, bool)
}

type reviewStore interface {
	sessionFinder
	CompareAndSetReviewPosition(name string, prev, next state.ReviewPosition) (bool, error)
}

type reviewLoop struct {
	messages *clitext.Messages
	lang     languageSource
	store    reviewStore
	seed     func(session, task string) bool
	trust    reviewloop.Trust
	now      func() time.Time
	observed keyedLocks
}

type keyedLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (k *keyedLocks) lock(key string) func() {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = make(map[string]*sync.Mutex)
	}
	mutex, ok := k.locks[key]
	if !ok {
		mutex = &sync.Mutex{}
		k.locks[key] = mutex
	}
	k.mu.Unlock()
	mutex.Lock()
	return mutex.Unlock
}

func observationKey(name string, number int) string {
	return fmt.Sprintf("%s#%d", name, number)
}

func newReviewLoop(messages *clitext.Messages, lang languageSource, store reviewStore, seed func(session, task string) bool) *reviewLoop {
	return &reviewLoop{
		messages: messages,
		lang:     lang,
		store:    store,
		seed:     seed,
		trust:    reviewTrust(),
	}
}

func reviewTrust() reviewloop.Trust {
	return reviewloop.NewTrust(splitSet(envx.Getenv(envReviewAuthors)))
}

func (r *reviewLoop) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

func (r *reviewLoop) expect(name string, number int, entry reviewloop.Pending) (reviewloop.Pending, error) {
	recorded := entry
	err := r.update(name, func(stored state.ReviewPosition) state.ReviewPosition {
		view := stored.ForRequest(number)
		recorded.PostedAt = r.clock()
		return state.ReviewPosition{
			Number:  number,
			Marks:   view.Marks,
			Own:     view.Own,
			Pending: reviewloop.Expect(view.Pending, recorded, recorded.PostedAt),
			Legacy:  view.Legacy,
		}
	})
	if err != nil {
		return reviewloop.Pending{}, err
	}
	return recorded, nil
}

func (r *reviewLoop) forget(name string, number int, entry reviewloop.Pending) error {
	return r.update(name, func(stored state.ReviewPosition) state.ReviewPosition {
		view := stored.ForRequest(number)
		return state.ReviewPosition{
			Number:  number,
			Marks:   view.Marks,
			Own:     view.Own,
			Pending: reviewloop.Forget(view.Pending, entry),
			Legacy:  view.Legacy,
		}
	})
}

func (r *reviewLoop) update(name string, edit func(stored state.ReviewPosition) state.ReviewPosition) error {
	for range reviewStoreAttempts {
		sess, ok := r.store.FindSession(name)
		if !ok {
			return errReviewNoSession
		}
		stored := sess.ReviewPosition()
		next := edit(stored)
		if next.Equal(stored) {
			return nil
		}
		claimed, err := r.store.CompareAndSetReviewPosition(name, stored, next)
		if err != nil {
			return err
		}
		if claimed {
			return nil
		}
	}
	return errReviewPositionBusy
}

func (r *reviewLoop) observe(ctx context.Context, name string, client contracts.Forge, ref contracts.PullRequestRef) {
	unlock := r.observed.lock(observationKey(name, ref.Number()))
	defer unlock()
	sess, ok := r.store.FindSession(name)
	if !ok {
		return
	}
	threads, err := r.threads(ctx, client, ref)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("session pull request review threads not read", "session", name, "err", err)
		}
		return
	}
	batch := r.plan(threads, sess.ReviewPosition(), ref.Number())
	if len(batch.Items) > 0 && !r.deliver(name, ref.Number(), batch) {
		slog.Warn("session review message not delivered, no live session", "session", name)
		return
	}
	if err := r.update(name, r.commit(threads, ref.Number(), batch)); err != nil {
		slog.Warn("session review position not recorded after delivery, the threads may be handed over again", "session", name, "err", err)
	}
}

func (r *reviewLoop) plan(threads []contracts.ReviewThread, stored state.ReviewPosition, number int) reviewloop.Batch {
	view := stored.ForRequest(number)
	marks := view.Marks
	if view.Legacy != nil {
		marks = reviewloop.Initialise(threads, view.Legacy)
	}
	own, _ := reviewloop.Settle(threads, view.Own, view.Pending, r.clock())
	return reviewloop.Plan(threads, marks, own, r.trust)
}

func (r *reviewLoop) commit(threads []contracts.ReviewThread, number int, batch reviewloop.Batch) func(state.ReviewPosition) state.ReviewPosition {
	return func(stored state.ReviewPosition) state.ReviewPosition {
		view := stored.ForRequest(number)
		own, pending := reviewloop.Settle(threads, view.Own, view.Pending, r.clock())
		marks := maps.Clone(view.Marks)
		if marks == nil {
			marks = make(map[string]reviewloop.Mark, len(batch.Marks))
		}
		maps.Copy(marks, batch.Marks)
		return state.ReviewPosition{Number: number, Marks: marks, Own: own, Pending: pending}
	}
}

func (r *reviewLoop) threads(ctx context.Context, client contracts.Forge, ref contracts.PullRequestRef) ([]contracts.ReviewThread, error) {
	ctx, cancel := context.WithTimeout(ctx, sessionPRForgeLimit)
	defer cancel()
	return forge.CollectPages(ctx, func(ctx context.Context, page contracts.PageRequest) (contracts.Page[contracts.ReviewThread], error) {
		return client.ReviewThreads(ctx, ref, page)
	})
}

func (r *reviewLoop) deliver(name string, number int, batch reviewloop.Batch) bool {
	if r.seed == nil {
		return false
	}
	lang := namedSessionLanguage(r.store, name, r.lang())
	text, err := r.messages.Text(lang, clitext.SessionPRReviewNudge, clitext.Params{
		clitext.ParamNumber: number,
		clitext.ParamCount:  len(batch.Items),
		clitext.ParamData:   reviewloop.Render(batch.Items),
	})
	if err != nil {
		slog.Error("session review message not rendered", "session", name, "err", err)
		return false
	}
	return r.seed(name, text)
}
