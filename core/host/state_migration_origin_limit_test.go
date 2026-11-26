package host

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

const (
	originTestLimit    = 300 * time.Millisecond
	originTestPatience = 15 * time.Second
)

func silentOriginURL(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(func() {
		server.CloseClientConnections()
		server.Close()
	})
	return server.URL + "/hung.git"
}

type blockingPRCredentials struct{}

func (blockingPRCredentials) Token(ctx context.Context, _ contracts.ForgeHost) (contracts.ForgeToken, error) {
	<-ctx.Done()
	return contracts.ForgeToken{}, ctx.Err()
}

func (blockingPRCredentials) Invalidate(context.Context, contracts.ForgeHost) {}

func planWithinPatience(t *testing.T, creds contracts.ForgeCredentials, repo string, bare worktree.BareSession) error {
	t.Helper()
	if _, hasDeadline := t.Context().Deadline(); hasDeadline {
		t.Fatal("the test context has a deadline, so it cannot show that the host limit is the one that cuts")
	}
	worktrees := worktree.NewWorktreer(clitext.NewMessages(), "inst")
	done := make(chan error, 1)
	go func() {
		_, err := planLocalBareRelocation(t.Context(), worktrees, creds, repo, bare, originTestLimit)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(originTestPatience):
		t.Fatalf("planning was still waiting on origin after %s, want it cut at its %s limit", originTestPatience, originTestLimit)
		return nil
	}
}

func TestPlanLocalBareRelocationCutsASilentOriginAtTheHostLimit(t *testing.T) {
	repo, from := bareSessionWithOrigin(t, silentOriginURL(t))
	err := planWithinPatience(t, nil, repo, worktree.BareSession{Name: "alpha", Path: from, PullRequest: 12})
	if failure, ok := errors.AsType[worktree.FailureError](err); !ok || failure.Key != clitext.WorktreeRelocateRemoteUnreachable {
		t.Fatalf("err = %v, want a %s refusal", err, clitext.WorktreeRelocateRemoteUnreachable)
	}
}

func TestPlanLocalBareRelocationCutsAForgeTokenThatNeverComesAtTheHostLimit(t *testing.T) {
	repo, from := bareSessionWithOrigin(t, "https://github.com/acme/app.git")
	err := planWithinPatience(t, blockingPRCredentials{}, repo, worktree.BareSession{Name: "alpha", Path: from, PullRequest: 12})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the host limit as its cause", err)
	}
	if failure, ok := errors.AsType[worktree.FailureError](err); !ok || failure.Key != clitext.WorktreeRelocateRemoteUnreachable {
		t.Fatalf("err = %v, want a %s refusal", err, clitext.WorktreeRelocateRemoteUnreachable)
	}
}
