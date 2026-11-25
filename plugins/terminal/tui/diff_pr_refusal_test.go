package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/health"
)

func pressPRWithHealth(t *testing.T, snapshot string) (*model, *recordingRunner, *prOpenBackend) {
	t.Helper()
	dir := initRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeAndCommitBase(t, dir)
	runner := &recordingRunner{remote: "git@github.com:o/r.git\n"}
	m, _ := newDiffModel(t, dir, runner.run, 120, 30)
	backend := newPROpenBackend(t, "unused")
	backend.health = snapshot
	m.tm = backend
	press(m, "ctrl+x", "d")
	press(m, "p")
	return m, runner, backend
}

func requireRefusal(t *testing.T, m *model, runner *recordingRunner, backend *prOpenBackend, want string) {
	t.Helper()
	if !strings.Contains(screen(m), want) {
		t.Fatalf("refusal %q missing:\n%s", want, screen(m))
	}
	if runner.pushes() != 0 || len(backend.dispatched) != 0 {
		t.Fatalf("pushes = %d, dispatched = %v, want nothing pushed or opened", runner.pushes(), backend.dispatched)
	}
}

func TestPRRefusesWhenTheForgeRateLimitIsSpent(t *testing.T) {
	report := health.ForgeFailure(&contracts.ForgeRateLimitedError{})

	m, runner, backend := pressPRWithHealth(t, healthWith(t, report))

	requireRefusal(t, m, runner, backend, "GitHub rate limit reached: try again later")
	if strings.Contains(screen(m), "herrscher connect github") {
		t.Fatalf("rate limit refusal tells to connect again:\n%s", screen(m))
	}
}

func TestPRRefusalGivesTheResetWhenTheReportCarriesIt(t *testing.T) {
	connection := contracts.ForgeConnection{Login: "octocat", RateLimit: contracts.ForgeRateLimit{Remaining: 12, Reset: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)}}
	report := health.ForgeReport(connection)
	facts := health.Facts{Forge: &health.Forge{Login: connection.Login, Quota: health.QuotaOf(connection.RateLimit)}}

	m, runner, backend := pressPRWithHealth(t, healthWithFacts(t, report, facts))

	requireRefusal(t, m, runner, backend, "GitHub rate limit reached, it resets at 2026-11-25T10:00:00Z")
}

func TestPRRefusalSaysTheForgeIsUnreachable(t *testing.T) {
	report := health.ForgeFailure(errors.New("dial tcp 140.82.112.3:443: connection refused"))

	m, runner, backend := pressPRWithHealth(t, healthWith(t, report))

	requireRefusal(t, m, runner, backend, "GitHub is unreachable from this host")
	if strings.Contains(screen(m), "herrscher connect github") {
		t.Fatalf("unreachable refusal tells to connect again:\n%s", screen(m))
	}
}

func TestPRRefusalSaysTheCredentialWasRefused(t *testing.T) {
	report := health.ForgeFailure(contracts.ErrForgeUnauthorized)

	m, runner, backend := pressPRWithHealth(t, healthWith(t, report))

	requireRefusal(t, m, runner, backend, "GitHub refused the saved credential: run `herrscher connect github`")
}

func TestPRRefusalSaysTheRepositoryIsUnreadable(t *testing.T) {
	report := health.WithRepoAccess(health.ForgeReport(contracts.ForgeConnection{Login: "octocat"}), "o/r", errors.New("404 not found"))

	m, runner, backend := pressPRWithHealth(t, healthWith(t, report))

	requireRefusal(t, m, runner, backend, "GitHub cannot read this repository with this connection")
}

func TestPRRefusalSaysTheForgeIsNotInstalled(t *testing.T) {
	data, err := health.Encode(health.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}

	m, runner, backend := pressPRWithHealth(t, string(data))

	requireRefusal(t, m, runner, backend, "the GitHub forge is not installed on this host")
}
