package host

import (
	"context"
	"log/slog"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/internal/forge"
	"github.com/Herrscherd/herrscher/core/internal/reviewloop"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const envReviewAuthors = "HERRSCHER_REVIEW_AUTHORS"

type reviewLoop struct {
	messages   *clitext.Messages
	lang       languageSource
	store      *state.State
	seed       func(session, task string) bool
	forgeLimit time.Duration
	trust      reviewloop.Trust
}

func reviewTrust() reviewloop.Trust {
	return reviewloop.NewTrust(splitSet(envx.Getenv(envReviewAuthors)))
}

func (r *reviewLoop) observe(ctx context.Context, name string, client contracts.Forge, ref contracts.PullRequestRef) {
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
	batch := reviewloop.Plan(threads, stored.SeenOf(ref.Number()), r.trust)
	next := state.ReviewPosition{Number: ref.Number(), Seen: batch.Seen}
	if next.Equal(stored) {
		return
	}
	claimed, err := r.store.CompareAndSetReviewPosition(name, stored, next)
	if err != nil {
		slog.Error("session review position not recorded, nothing sent", "session", name, "err", err)
		return
	}
	if !claimed || len(batch.Items) == 0 {
		return
	}
	if r.deliver(name, ref.Number(), batch) {
		return
	}
	slog.Warn("session review message not delivered, no live session", "session", name)
	if _, err := r.store.CompareAndSetReviewPosition(name, next, stored); err != nil {
		slog.Error("session review position not returned", "session", name, "err", err)
	}
}

func (r *reviewLoop) threads(ctx context.Context, client contracts.Forge, ref contracts.PullRequestRef) ([]contracts.ReviewThread, error) {
	ctx, cancel := context.WithTimeout(ctx, r.forgeLimit)
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
