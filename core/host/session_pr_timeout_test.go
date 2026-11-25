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

func prRegistryWithForgeLimit(t *testing.T, st *state.State, opener forgeOpener, limit time.Duration) *cli.Registry {
	t.Helper()
	reg := cli.NewRegistry(clitext.NewMessages())
	if err := newSessionPRs(clitext.NewMessages(), fixedLanguage(i18n.English), st, opener, nil, limit).addTo(reg); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestPROpenGivesUpWhenTheForgeStaysSilent(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	reg := prRegistryWithForgeLimit(t, st, prForgeOpener(silentPRForge{}), prSilentForgeLimit)

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
	reg := prRegistryWithForgeLimit(t, st, prForgeOpener(silentPRForge{}), prSilentForgeLimit)

	_, err := runPR(t, reg, verbPRStatus, "--name", "alpha")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("status against a silent forge = %v, want the deadline", err)
	}
}
