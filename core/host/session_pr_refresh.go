package host

import (
	"context"
	"log/slog"
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type refreshLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newRefreshLocks() *refreshLocks {
	return &refreshLocks{locks: make(map[string]*sync.Mutex)}
}

func (l *refreshLocks) lock(name string) func() {
	l.mu.Lock()
	guard, ok := l.locks[name]
	if !ok {
		guard = new(sync.Mutex)
		l.locks[name] = guard
	}
	l.mu.Unlock()
	guard.Lock()
	return guard.Unlock
}

func (p sessionPRs) status(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	target, err := p.target(ctx, input.Get(paramName))
	if err != nil {
		return contracts.Result{}, err
	}
	pullRequest, unrecorded, err := p.refresh(ctx, target)
	if err != nil {
		return contracts.Result{}, err
	}
	if input.JSON {
		return cli.Data(marshalJSON(prStatusView{
			Number:     target.ref.Number(),
			Title:      pullRequest.Title,
			URL:        pullRequest.URL,
			State:      pullRequest.State.String(),
			Draft:      pullRequest.Draft,
			Merge:      pullRequest.Merge.String(),
			Head:       pullRequest.Head,
			Base:       pullRequest.Base,
			HeadSHA:    pullRequest.HeadSHA.String(),
			Unrecorded: unrecorded,
		}))
	}
	stateText, err := p.stateWord(target.language, pullRequest.State)
	if err != nil {
		return contracts.Result{}, err
	}
	mergeText, err := p.mergeWord(target.language, pullRequest.Merge)
	if err != nil {
		return contracts.Result{}, err
	}
	text, err := p.messages.Text(target.language, clitext.SessionPRStatus, clitext.Params{
		clitext.ParamNumber: target.ref.Number(),
		clitext.ParamState:  stateText,
		clitext.ParamMerge:  mergeText,
		clitext.ParamTitle:  escapeLine(pullRequest.Title),
		clitext.ParamHead:   escapeLine(pullRequest.Head),
		clitext.ParamBase:   escapeLine(pullRequest.Base),
		clitext.ParamURL:    escapeLine(pullRequest.URL),
	})
	if err != nil {
		return contracts.Result{}, err
	}
	if unrecorded {
		notice, err := p.unrecordedNotice(target)
		if err != nil {
			return contracts.Result{}, err
		}
		text += "\n" + notice
	}
	return cli.Data(text, nil)
}

func (p sessionPRs) refresh(ctx context.Context, target prTarget) (contracts.PullRequest, bool, error) {
	unlock := p.refreshes.lock(target.name)
	defer unlock()
	pullRequest, err := target.forge.PullRequest(ctx, target.ref, contracts.Revision(""))
	if err != nil {
		return contracts.PullRequest{}, false, p.failure(target.language, err)
	}
	record := state.PullRequestRecord{Number: target.ref.Number(), State: pullRequest.State, Draft: pullRequest.Draft, URL: pullRequest.URL}
	if err := p.store.RefreshPullRequest(target.name, record); err != nil {
		slog.Warn("session pull request state not saved", "session", target.name, "err", err)
		return pullRequest, true, nil
	}
	return pullRequest, false, nil
}

func (p sessionPRs) unrecordedNotice(target prTarget) (string, error) {
	return p.messages.Text(target.language, clitext.SessionPRRefreshUnrecorded, clitext.Params{
		clitext.ParamName:   escapeLine(target.name),
		clitext.ParamNumber: target.ref.Number(),
	})
}
