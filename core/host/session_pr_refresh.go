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
	slots map[string]chan struct{}
}

func newRefreshLocks() *refreshLocks {
	return &refreshLocks{slots: make(map[string]chan struct{})}
}

func (l *refreshLocks) acquire(ctx context.Context, name string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slot := l.slot(name)
	select {
	case slot <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-slot
			return nil, err
		}
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *refreshLocks) slot(name string) chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	slot, ok := l.slots[name]
	if !ok {
		slot = make(chan struct{}, 1)
		l.slots[name] = slot
	}
	return slot
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
			State:      clitext.PullRequestStateID(pullRequest.State),
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
	ctx, cancel := context.WithTimeout(ctx, p.forgeLimit)
	defer cancel()
	release, err := p.refreshes.acquire(ctx, target.name)
	if err != nil {
		return contracts.PullRequest{}, false, p.failure(target.language, err)
	}
	defer release()
	pullRequest, err := target.forge.PullRequest(ctx, target.ref, contracts.Revision(""))
	if err != nil {
		return contracts.PullRequest{}, false, p.failure(target.language, err)
	}
	record := state.NewPullRequestRecord(target.ref.Number(), pullRequest)
	if err := p.recordPullRequest(target.name, record); err != nil {
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
