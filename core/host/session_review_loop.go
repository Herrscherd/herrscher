package host

import (
	"context"
	"errors"
	"log/slog"
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
	errReviewPositionBusy = errors.New("session review position kept changing")
	errReviewNoSession    = errors.New("no such session for the review position")
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

func (r *reviewLoop) expect(name string, number int, entry reviewloop.Pending) error {
	for range reviewStoreAttempts {
		sess, ok := r.store.FindSession(name)
		if !ok {
			return errReviewNoSession
		}
		stored := sess.ReviewPosition()
		view := stored.ForRequest(number)
		entry.PostedAt = r.clock()
		next := state.ReviewPosition{
			Number:  number,
			Marks:   view.Marks,
			Own:     view.Own,
			Pending: reviewloop.Expect(view.Pending, entry, entry.PostedAt),
			Legacy:  view.Legacy,
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
	for range reviewStoreAttempts {
		sess, ok := r.store.FindSession(name)
		if !ok {
			return
		}
		stored := sess.ReviewPosition()
		threads, err := r.threads(ctx, client, ref)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("session pull request review threads not read", "session", name, "err", err)
			}
			return
		}
		prior, next, batch := r.plan(threads, stored, ref.Number())
		if next.Equal(stored) {
			return
		}
		claimed, err := r.store.CompareAndSetReviewPosition(name, stored, next)
		if err != nil {
			slog.Error("session review position not recorded, nothing sent", "session", name, "err", err)
			return
		}
		if !claimed {
			continue
		}
		if len(batch.Items) == 0 || r.deliver(name, ref.Number(), batch) {
			return
		}
		slog.Warn("session review message not delivered, no live session", "session", name)
		if _, err := r.store.CompareAndSetReviewPosition(name, next, prior); err != nil {
			slog.Error("session review position not returned", "session", name, "err", err)
		}
		return
	}
	slog.Warn("session review position busy, observation skipped", "session", name)
}

func (r *reviewLoop) plan(threads []contracts.ReviewThread, stored state.ReviewPosition, number int) (state.ReviewPosition, state.ReviewPosition, reviewloop.Batch) {
	view := stored.ForRequest(number)
	marks := view.Marks
	if view.Legacy != nil {
		marks = reviewloop.Initialise(threads, view.Legacy)
	}
	own, pending := reviewloop.Settle(threads, view.Own, view.Pending, r.clock())
	batch := reviewloop.Plan(threads, marks, own, r.trust)
	prior := state.ReviewPosition{Number: number, Marks: marks, Own: own, Pending: pending}
	next := state.ReviewPosition{Number: number, Marks: batch.Marks, Own: own, Pending: pending}
	return prior, next, batch
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
