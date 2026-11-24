package terminal

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/forgecreds"
	"github.com/Herrscherd/herrscher/core/forgeport"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

type githubPullRequests struct {
	creds contracts.ForgeCredentials
}

func newPullRequests() githubPullRequests {
	return githubPullRequests{creds: forgecreds.New(host.DefaultStatePath())}
}

func (p githubPullRequests) OpenPullRequest(ctx context.Context, draft tui.PullRequestDraft) (int, error) {
	repo, err := contracts.ParseRepoRef(draft.Remote)
	if err != nil {
		return 0, err
	}
	if repo.Host().String() != forgeport.GitHubHost {
		return 0, contracts.ErrForgeWrongHost
	}
	forge, err := forgeport.Open(ctx, forgeport.KindGitHub, repo.Host(), p.creds)
	if err != nil {
		return 0, err
	}
	pullRequest, err := forge.OpenPullRequest(ctx, contracts.PullRequestDraft{Repo: repo, Head: draft.Head, Base: draft.Base, Title: draft.Title, Body: draft.Body})
	if err != nil {
		return 0, err
	}
	return pullRequest.Ref.Number(), nil
}
