package terminal

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

const githubHost = "github.com"

type forgeOpener func(ctx context.Context, host contracts.ForgeHost) (contracts.Forge, error)

type githubPullRequests struct {
	open forgeOpener
}

func newPullRequests(open forgeOpener) githubPullRequests {
	return githubPullRequests{open: open}
}

func (p githubPullRequests) OpenPullRequest(ctx context.Context, draft tui.PullRequestDraft) (int, error) {
	repo, err := contracts.ParseRepoRef(draft.Remote)
	if err != nil {
		return 0, err
	}
	if repo.Host().String() != githubHost {
		return 0, contracts.ErrForgeWrongHost
	}
	forge, err := p.open(ctx, repo.Host())
	if err != nil {
		return 0, err
	}
	pullRequest, err := forge.OpenPullRequest(ctx, contracts.PullRequestDraft{Repo: repo, Head: draft.Head, Base: draft.Base, Title: draft.Title, Body: draft.Body})
	if err != nil {
		return 0, err
	}
	return pullRequest.Ref.Number(), nil
}
