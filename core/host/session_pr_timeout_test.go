package host

import (
	"context"
	"errors"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const prSilentForgeLimit = 20 * time.Millisecond

type silentPRForge struct {
	contracts.Forge
}

func (silentPRForge) OpenPullRequest(ctx context.Context, _ contracts.PullRequestDraft) (contracts.PullRequest, error) {
	<-ctx.Done()
	return contracts.PullRequest{}, ctx.Err()
}

func (silentPRForge) PullRequest(ctx context.Context, _ contracts.PullRequestRef, _ contracts.Revision) (contracts.PullRequest, error) {
	<-ctx.Done()
	return contracts.PullRequest{}, ctx.Err()
}

func prRegistryWithSilentLimit(t *testing.T, st *state.State, opener forgeOpener) *cli.Registry {
	t.Helper()
	reg := cli.NewRegistry(clitext.NewMessages())
	if err := newSessionPRs(clitext.NewMessages(), fixedLanguage(i18n.English), st, opener, nil, prSilentForgeLimit).addTo(reg); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestPROpenGivesUpWhenTheForgeStaysSilent(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	reg := prRegistryWithSilentLimit(t, st, prForgeOpener(silentPRForge{}))

	_, err := runPR(t, reg, verbPROpen, "--name", "alpha", "--title", "Titre")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("open against a silent forge = %v, want the deadline", err)
	}
}

func TestPRStatusGivesUpWhenTheForgeStaysSilent(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	reg := prRegistryWithSilentLimit(t, st, prForgeOpener(silentPRForge{}))

	_, err := runPR(t, reg, verbPRStatus, "--name", "alpha")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("status against a silent forge = %v, want the deadline", err)
	}
}

func TestPRChecksGivesUpWhenTheForgeStaysSilent(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	reg := prRegistryWithSilentLimit(t, st, prForgeOpener(unansweredReadsForge{&fakePRForge{}}))

	_, err := runPR(t, reg, verbPRChecks, "--name", "alpha")
	if errors.Is(err, errPRForgeUnbounded) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("checks against a silent forge = %v, want the deadline", err)
	}
}

func TestPRCommentsGivesUpWhenTheForgeStaysSilent(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	reg := prRegistryWithSilentLimit(t, st, prForgeOpener(unansweredReadsForge{&fakePRForge{}}))

	_, err := runPR(t, reg, verbPRComments, "--name", "alpha")
	if errors.Is(err, errPRForgeUnbounded) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("comments against a silent forge = %v, want the deadline", err)
	}
}

var errPRForgeUnbounded = errors.New("forge read was not bounded by the command limit")

const prUnboundedGuard = 5 * time.Second

type unansweredReadsForge struct {
	*fakePRForge
}

func (unansweredReadsForge) Checks(ctx context.Context, _ contracts.ForgeRepoRef, _ contracts.CommitSHA, _ contracts.PageRequest) (contracts.Page[contracts.Check], error) {
	return contracts.Page[contracts.Check]{}, waitForForgeRead(ctx)
}

func (unansweredReadsForge) ReviewThreads(ctx context.Context, _ contracts.PullRequestRef, _ contracts.PageRequest) (contracts.Page[contracts.ReviewThread], error) {
	return contracts.Page[contracts.ReviewThread]{}, waitForForgeRead(ctx)
}

func waitForForgeRead(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(prUnboundedGuard):
		return errPRForgeUnbounded
	}
}
