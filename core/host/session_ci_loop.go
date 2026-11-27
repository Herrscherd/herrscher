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
	stored := sess.CINudge()
	base := stored
	if base.Number != pull.Ref.Number() {
		base = state.CINudge{Number: pull.Ref.Number()}
	}
	snapshot := ciloop.Snapshot{
		State:    pull.State,
		Draft:    pull.Draft,
		Checks:   pull.Checks,
		Attempts: ciloop.Attempts(base.Attempts),
		Reported: base.Head == pull.HeadSHA.String(),
	}
	switch ciloop.Decide(snapshot) {
	case ciloop.ActionFix:
		c.fix(ctx, name, forge, pull, stored, base)
	case ciloop.ActionStop:
		c.announce(name, pull, stored, base, clitext.SessionCIStop, clitext.Params{
			clitext.ParamNumber: pull.Ref.Number(),
			clitext.ParamMax:    int(ciloop.MaxAttempts),
		})
	case ciloop.ActionWait, ciloop.ActionIdle, ciloop.ActionHuman:
	}
}

func (c *ciLoop) fix(ctx context.Context, name string, forge contracts.Forge, pull contracts.PullRequest, stored, base state.CINudge) {
	ctx, cancel := context.WithTimeout(ctx, c.forgeLimit)
	defer cancel()
	failure, err := c.failure(ctx, forge, pull)
	if err != nil {
		slog.Warn("session pull request checks not read", "session", name, "err", err)
		return
	}
	if !failure.found {
		c.announce(name, pull, stored, base, clitext.SessionCIUnlocated, clitext.Params{
			clitext.ParamNumber: pull.Ref.Number(),
		})
		return
	}
	lang := namedSessionLanguage(c.store, name, c.lang())
	text, err := c.nudgeText(lang, pull, failure, base.Attempts+1)
	if err != nil {
		slog.Error("session ci nudge not rendered", "session", name, "err", err)
		return
	}
	c.deliver(name, stored, c.reserved(pull, base.Attempts+1), text)
}

func (c *ciLoop) nudgeText(lang i18n.Language, pull contracts.PullRequest, failure ciFailure, attempt int) (string, error) {
	logText := failure.log
	if logText == "" {
		stand, err := c.messages.Text(lang, clitext.SessionCINoLog, nil)
		if err != nil {
			return "", err
		}
		logText = stand
	}
	return c.messages.Text(lang, clitext.SessionCINudge, clitext.Params{
		clitext.ParamNumber:  pull.Ref.Number(),
		clitext.ParamAttempt: attempt,
		clitext.ParamMax:     int(ciloop.MaxAttempts),
		clitext.ParamLog:     ciloop.Payload(escapeLine(failure.check.Name), escapeLine(failure.check.URL), logText),
	})
}

func (c *ciLoop) announce(name string, pull contracts.PullRequest, stored, base state.CINudge, key clitext.Key, params clitext.Params) {
	lang := namedSessionLanguage(c.store, name, c.lang())
	text, err := c.messages.Text(lang, key, params)
	if err != nil {
		slog.Error("session ci message not rendered", "session", name, "err", err)
		return
	}
	c.deliver(name, stored, c.reserved(pull, base.Attempts), text)
}

func (c *ciLoop) reserved(pull contracts.PullRequest, attempts int) state.CINudge {
	return state.CINudge{Number: pull.Ref.Number(), Attempts: attempts, Head: pull.HeadSHA.String()}
}

func (c *ciLoop) deliver(name string, stored, reserved state.CINudge, text string) {
	claimed, err := c.store.CompareAndSetCINudge(name, stored, reserved)
	if err != nil {
		slog.Error("session ci message not recorded, nothing sent", "session", name, "err", err)
		return
	}
	if !claimed {
		return
	}
	if c.seed(name, text) {
		return
	}
	slog.Warn("session ci message not delivered, no live session", "session", name)
	if _, err := c.store.CompareAndSetCINudge(name, reserved, stored); err != nil {
		slog.Error("session ci attempt not returned", "session", name, "err", err)
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
	failure.log = string(raw)
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
