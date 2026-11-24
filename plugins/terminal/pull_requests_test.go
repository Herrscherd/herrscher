package terminal

import (
	"context"
	"errors"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

type recordingForge struct {
	contracts.Forge

	draft contracts.PullRequestDraft
	err   error
}

func (f *recordingForge) OpenPullRequest(_ context.Context, draft contracts.PullRequestDraft) (contracts.PullRequest, error) {
	f.draft = draft
	return contracts.PullRequest{}, f.err
}

func TestOpenPullRequestRefusesNonGitHubOriginWithoutOpeningAForge(t *testing.T) {
	cases := map[string]string{
		"gitlab":     "gitlab.com/owner/repo",
		"enterprise": "ghe.example.com/owner/repo",
		"lookalike":  "github.com.evil.test/owner/repo",
	}
	for name, remote := range cases {
		t.Run(name, func(t *testing.T) {
			opened := false
			open := func(context.Context, contracts.ForgeHost) (contracts.Forge, error) {
				opened = true
				return &recordingForge{}, nil
			}
			_, err := newPullRequests(open).OpenPullRequest(context.Background(), tui.PullRequestDraft{Remote: remote})
			if !errors.Is(err, contracts.ErrForgeWrongHost) {
				t.Fatalf("err = %v, want ErrForgeWrongHost", err)
			}
			if opened {
				t.Fatal("a forge was opened for a non-GitHub origin")
			}
		})
	}
}

func TestOpenPullRequestOpensGitHubThroughTheInjectedOpener(t *testing.T) {
	forge := &recordingForge{}
	var opened contracts.ForgeHost
	open := func(_ context.Context, host contracts.ForgeHost) (contracts.Forge, error) {
		opened = host
		return forge, nil
	}
	draft := tui.PullRequestDraft{Remote: "github.com/owner/repo", Head: "feat", Base: "master", Title: "t", Body: "b"}

	if _, err := newPullRequests(open).OpenPullRequest(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	if opened.String() != githubHost {
		t.Fatalf("opened host = %q, want %q", opened.String(), githubHost)
	}
	if forge.draft.Head != "feat" || forge.draft.Base != "master" || forge.draft.Title != "t" || forge.draft.Body != "b" {
		t.Fatalf("draft sent = %+v", forge.draft)
	}
}

func TestNewPullRequestsWithoutForgeIsNoOpener(t *testing.T) {
	opener := newPullRequests(nil)
	if opener != nil {
		t.Fatalf("opener = %T, want nil so the window reports that no forge is wired", opener)
	}
}

func TestOpenPullRequestReturnsTheOpenerError(t *testing.T) {
	boom := errors.New("not connected")
	open := func(context.Context, contracts.ForgeHost) (contracts.Forge, error) { return nil, boom }

	_, err := newPullRequests(open).OpenPullRequest(context.Background(), tui.PullRequestDraft{Remote: "github.com/owner/repo"})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the opener error", err)
	}
}
