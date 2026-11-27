package host

import (
	"context"
	"log/slog"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/ciloop"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const ciChecksPageLimit = 5

type ciLoop struct {
	messages   *clitext.Messages
	lang       languageSource
	store      *state.State
	seed       func(session, task string) bool
	forgeLimit time.Duration
}

type ciFailure struct {
	check contracts.Check
	log   string
	found bool
}

func (c *ciLoop) observe(ctx context.Context, name string, forge contracts.Forge, pull contracts.PullRequest) {
	sess, ok := c.store.FindSession(name)
	if !ok {
		return
	}
	number := pull.Ref.Number()
	nudge := sess.CINudge()
	if nudge.Number != number {
		nudge = state.CINudge{Number: number}
	}
	snapshot := ciloop.Snapshot{
		State:    pull.State,
		Draft:    pull.Draft,
		Checks:   pull.Checks,
		Attempts: ciloop.Attempts(nudge.Attempts),
		Reported: nudge.Head == pull.HeadSHA.String(),
	}
	switch ciloop.Decide(snapshot) {
	case ciloop.ActionFix:
		c.fix(ctx, name, forge, pull, nudge)
	case ciloop.ActionStop:
		c.stop(name, pull, nudge)
	case ciloop.ActionWait, ciloop.ActionIdle, ciloop.ActionHuman:
	}
}

func (c *ciLoop) fix(ctx context.Context, name string, forge contracts.Forge, pull contracts.PullRequest, nudge state.CINudge) {
	ctx, cancel := context.WithTimeout(ctx, c.forgeLimit)
	defer cancel()
	failure, err := c.failure(ctx, forge, pull)
	if err != nil {
		slog.Warn("session pull request checks not read", "session", name, "err", err)
		return
	}
	if !failure.found {
		return
	}
	lang := namedSessionLanguage(c.store, name, c.lang())
	logText, err := c.logText(lang, failure)
	if err != nil {
		slog.Error("session ci nudge not rendered", "session", name, "err", err)
		return
	}
	text, err := c.messages.Text(lang, clitext.SessionCINudge, clitext.Params{
		clitext.ParamNumber:  pull.Ref.Number(),
		clitext.ParamAttempt: nudge.Attempts + 1,
		clitext.ParamMax:     int(ciloop.MaxAttempts),
		clitext.ParamCheck:   escapeLine(failure.check.Name),
		clitext.ParamURL:     escapeLine(failure.check.URL),
		clitext.ParamLog:     logText,
	})
	if err != nil {
		slog.Error("session ci nudge not rendered", "session", name, "err", err)
		return
	}
	if !c.seed(name, text) {
		slog.Warn("session ci nudge not delivered, no live session", "session", name)
		return
	}
	c.record(name, state.CINudge{Number: pull.Ref.Number(), Attempts: nudge.Attempts + 1, Head: pull.HeadSHA.String()})
}

func (c *ciLoop) stop(name string, pull contracts.PullRequest, nudge state.CINudge) {
	lang := namedSessionLanguage(c.store, name, c.lang())
	text, err := c.messages.Text(lang, clitext.SessionCIStop, clitext.Params{
		clitext.ParamNumber: pull.Ref.Number(),
		clitext.ParamMax:    int(ciloop.MaxAttempts),
	})
	if err != nil {
		slog.Error("session ci stop not rendered", "session", name, "err", err)
		return
	}
	if !c.seed(name, text) {
		slog.Warn("session ci stop not delivered, no live session", "session", name)
		return
	}
	c.record(name, state.CINudge{Number: pull.Ref.Number(), Attempts: nudge.Attempts, Head: pull.HeadSHA.String()})
}

func (c *ciLoop) record(name string, nudge state.CINudge) {
	if err := c.store.SetCINudge(name, nudge); err != nil {
		slog.Warn("session ci nudge not saved", "session", name, "err", err)
	}
}

func (c *ciLoop) failure(ctx context.Context, forge contracts.Forge, pull contracts.PullRequest) (ciFailure, error) {
	checks, err := c.checks(ctx, forge, pull)
	if err != nil {
		return ciFailure{}, err
	}
	for _, check := range checks {
		if contracts.SummarizeChecks([]contracts.Check{check}) == contracts.ChecksFailed {
			return c.withLog(ctx, forge, check), nil
		}
	}
	return ciFailure{}, nil
}

func (c *ciLoop) withLog(ctx context.Context, forge contracts.Forge, check contracts.Check) ciFailure {
	failure := ciFailure{check: check, found: true}
	if check.Job == nil {
		return failure
	}
	raw, err := forge.JobLog(ctx, *check.Job, ciloop.LogTailBytes)
	if err != nil {
		slog.Warn("session ci job log not read", "check", check.Name, "err", err)
		return failure
	}
	failure.log = ciloop.SanitizeLog(raw)
	return failure
}

func (c *ciLoop) checks(ctx context.Context, forge contracts.Forge, pull contracts.PullRequest) ([]contracts.Check, error) {
	var all []contracts.Check
	var cursor contracts.PageCursor
	for range ciChecksPageLimit {
		page, err := forge.Checks(ctx, pull.Ref.Repo(), pull.HeadSHA, contracts.PageRequest{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	return all, nil
}

func (c *ciLoop) logText(lang i18n.Language, failure ciFailure) (string, error) {
	if failure.log == "" {
		return c.messages.Text(lang, clitext.SessionCINoLog, nil)
	}
	return failure.log, nil
}
