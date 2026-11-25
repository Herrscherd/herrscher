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
	report, _ := health.ForgeFailure(&contracts.ForgeRateLimitedError{})

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

	requireRefusal(t, m, runner, backend, "GitHub rate limit reached, it resets at ")
	requireScreenShows(t, m, "10:00 UTC")
}

func TestPRRefusalGivesTheResetWhenTheLimitIsSpent(t *testing.T) {
	limited := &contracts.ForgeRateLimitedError{Reset: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)}
	report, facts := health.ForgeFailure(limited)

	m, runner, backend := pressPRWithHealth(t, healthWithFacts(t, report, facts))

	requireRefusal(t, m, runner, backend, "GitHub rate limit reached, it resets at ")
	requireScreenShows(t, m, "10:00 UTC")
}

func requireScreenShows(t *testing.T, m *model, want string) {
	t.Helper()
	if !strings.Contains(screen(m), want) {
		t.Fatalf("screen lacks %q:\n%s", want, screen(m))
	}
}

func TestResetLabelShowsTheUTCClockAndTheDayOnlyWhenItIsNotToday(t *testing.T) {
	now := time.Date(2026, 11, 25, 8, 30, 0, 0, time.UTC)
	paris := time.FixedZone("CET", 60*60)
	tests := []struct {
		name  string
		reset time.Time
		want  string
	}{
		{name: "same UTC day shows the clock", reset: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC), want: "10:00 UTC"},
		{name: "another day adds the date", reset: time.Date(2026, 11, 26, 7, 15, 0, 0, time.UTC), want: "2026-11-26 07:15 UTC"},
		{name: "another zone is shown in UTC", reset: time.Date(2026, 11, 25, 12, 0, 0, 0, paris), want: "11:00 UTC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resetLabel(tt.reset, now); got != tt.want {
				t.Errorf("resetLabel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPRRefusalSaysTheForgeIsUnreachable(t *testing.T) {
	report, _ := health.ForgeFailure(errors.New("dial tcp 140.82.112.3:443: connection refused"))

	m, runner, backend := pressPRWithHealth(t, healthWith(t, report))

	requireRefusal(t, m, runner, backend, "GitHub is unreachable from this host")
	if strings.Contains(screen(m), "herrscher connect github") {
		t.Fatalf("unreachable refusal tells to connect again:\n%s", screen(m))
	}
}

func TestPRRefusalSaysTheCredentialWasRefused(t *testing.T) {
	report, _ := health.ForgeFailure(contracts.ErrForgeUnauthorized)

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
