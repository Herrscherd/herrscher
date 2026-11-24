package host

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/health"
)

type fakeForge struct {
	contracts.Forge

	connection contracts.ForgeConnection
	connErr    error
	connects   int
	permission contracts.RepoPermission
	accessErr  error
	asked      []contracts.ForgeRepoRef
}

func (f *fakeForge) Connection(context.Context) (contracts.ForgeConnection, error) {
	f.connects++
	return f.connection, f.connErr
}

func (f *fakeForge) RepoAccess(_ context.Context, repo contracts.ForgeRepoRef) (contracts.RepoPermission, error) {
	f.asked = append(f.asked, repo)
	return f.permission, f.accessErr
}

type manualClock struct {
	now time.Time
}

func (c *manualClock) Now() time.Time {
	return c.now
}

func openingWith(forge contracts.Forge, err error) forgeOpener {
	return func(context.Context, contracts.ForgeHost) (contracts.Forge, error) {
		return forge, err
	}
}

func probeOnce(t *testing.T, open forgeOpener, origin string) (contracts.ProbeReport, health.Facts) {
	t.Helper()
	return githubRun(open, origin)(context.Background(), false)
}

func connectedAs(remaining int) *fakeForge {
	return &fakeForge{connection: contracts.ForgeConnection{Login: "octo", Source: contracts.TokenSourceSaved, RateLimit: contracts.ForgeRateLimit{Remaining: remaining, Reset: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)}}}
}

func sameNotice(got, want contracts.Notice) bool {
	return got.Code == want.Code && slices.Equal(got.Args, want.Args)
}

func TestForgeStateFollowsTheConnection(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		want       contracts.HealthState
		wantDetail contracts.Code
	}{
		{name: "not connected warns", err: fmt.Errorf("probe: %w", contracts.ErrForgeNotConnected), want: contracts.HealthDegraded, wantDetail: contracts.CodeHealthNotLoggedIn},
		{name: "refused credential is an error", err: contracts.ErrForgeUnauthorized, want: contracts.HealthDown, wantDetail: contracts.CodeHealthCredentialsRefused},
		{name: "rate limited is an error", err: &contracts.ForgeRateLimitedError{}, want: contracts.HealthDown, wantDetail: contracts.CodeHealthRateLimited},
		{name: "other failure is unreachable", err: errors.New("dial tcp: refused"), want: contracts.HealthDown, wantDetail: contracts.CodeHealthUnreachable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, _ := probeOnce(t, openingWith(&fakeForge{connErr: tt.err}, nil), "")
			if report.State != tt.want {
				t.Errorf("state = %v, want %v", report.State, tt.want)
			}
			if report.Detail.Code != tt.wantDetail {
				t.Errorf("detail = %v, want %v", report.Detail.Code, tt.wantDetail)
			}
		})
	}
}

func TestNotConnectedPointsAtTheConnectCommand(t *testing.T) {
	report, _ := probeOnce(t, openingWith(&fakeForge{connErr: contracts.ErrForgeNotConnected}, nil), "")
	if !sameNotice(report.Remedy, gitHubConnectRemedy()) {
		t.Errorf("remedy = %+v, want the connect command", report.Remedy)
	}
}

func TestConnectedAccountReportsItsLogin(t *testing.T) {
	report, facts := probeOnce(t, openingWith(connectedAs(4000), nil), "")
	want := contracts.Notice{Code: contracts.CodeHealthSignedInAs, Args: []contracts.Arg{contracts.Text("Name", "octo")}}
	if report.State != contracts.HealthOK || !sameNotice(report.Detail, want) {
		t.Fatalf("report = %+v, want ok signed in as octo", report)
	}
	if facts.Forge == nil || facts.Forge.Source != contracts.TokenSourceSaved || facts.Forge.Quota == nil || facts.Forge.Quota.Remaining != 4000 {
		t.Errorf("facts = %+v, want the source and the quota", facts.Forge)
	}
}

func TestLowRateLimitWarnsBelowOneHundredRequests(t *testing.T) {
	tests := []struct {
		name      string
		remaining int
		want      contracts.HealthState
	}{
		{name: "exactly one hundred is fine", remaining: 100, want: contracts.HealthOK},
		{name: "just under one hundred warns", remaining: 99, want: contracts.HealthDegraded},
		{name: "exhausted warns", remaining: 0, want: contracts.HealthDegraded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, _ := probeOnce(t, openingWith(connectedAs(tt.remaining), nil), "")
			if report.State != tt.want {
				t.Errorf("state = %v, want %v", report.State, tt.want)
			}
		})
	}
}

func TestLowRateLimitKeepsTheLoginBesideTheWarning(t *testing.T) {
	report, facts := probeOnce(t, openingWith(connectedAs(12), nil), "")
	if report.State != contracts.HealthDegraded || report.Detail.Code != contracts.CodeHealthRateLimited {
		t.Errorf("report = %+v, want degraded with the rate limit warning", report)
	}
	if facts.Forge == nil || facts.Forge.Login != "octo" {
		t.Errorf("facts = %+v, want the login kept beside the warning", facts.Forge)
	}
}

func TestUnknownQuotaIsNeitherZeroNorAWarning(t *testing.T) {
	forge := &fakeForge{connection: contracts.ForgeConnection{Login: "octo", Source: contracts.TokenSourceSaved}}
	report, facts := probeOnce(t, openingWith(forge, nil), "")
	if report.State != contracts.HealthOK {
		t.Errorf("state = %v, want ok: a forge that sent no rate limit is not exhausted", report.State)
	}
	if facts.Forge == nil || facts.Forge.Quota != nil {
		t.Errorf("facts = %+v, want no quota", facts.Forge)
	}
}

func TestNoSessionRepoAsksForNoPermission(t *testing.T) {
	forge := connectedAs(4000)
	_, facts := probeOnce(t, openingWith(forge, nil), "")
	if len(forge.asked) != 0 || facts.Forge.Repo != "" {
		t.Errorf("asked %v and reported repo %q, want neither", forge.asked, facts.Forge.Repo)
	}
}

func TestNonGitHubOriginIsSkipped(t *testing.T) {
	forge := connectedAs(4000)
	_, facts := probeOnce(t, openingWith(forge, nil), "https://gitlab.com/octo/herrscher.git")
	if len(forge.asked) != 0 || facts.Forge.Repo != "" {
		t.Errorf("asked %v and reported repo %q, want neither", forge.asked, facts.Forge.Repo)
	}
}

func TestSessionRepoReportsThePermissionOnGitHub(t *testing.T) {
	forge := connectedAs(4000)
	forge.permission = contracts.PermissionWrite
	_, facts := probeOnce(t, openingWith(forge, nil), "git@github.com:octo/herrscher.git")
	if len(forge.asked) != 1 {
		t.Fatalf("RepoAccess asked %d times, want once", len(forge.asked))
	}
	if facts.Forge.Repo != "octo/herrscher" || facts.Forge.Permission == nil || *facts.Forge.Permission != contracts.PermissionWrite {
		t.Errorf("repo = %q permission = %v, want octo/herrscher write", facts.Forge.Repo, facts.Forge.Permission)
	}
}

func TestUnreadableRepoPermissionIsAWarningWithItsReason(t *testing.T) {
	forge := connectedAs(4000)
	forge.accessErr = contracts.ErrForgeNotFound
	report, facts := probeOnce(t, openingWith(forge, nil), "https://github.com/octo/herrscher")
	if report.State != contracts.HealthDegraded {
		t.Errorf("state = %v, want degraded: a right that cannot be read is a warning", report.State)
	}
	if !sameNotice(report.Detail, repoAccessWarning(contracts.ErrForgeNotFound).Detail) {
		t.Errorf("detail = %+v, want the reason of the failed right check", report.Detail)
	}
	if facts.Forge.Repo != "octo/herrscher" || facts.Forge.Permission != nil {
		t.Errorf("repo = %q permission = %v, want octo/herrscher with no permission", facts.Forge.Repo, facts.Forge.Permission)
	}
}

func TestWatchedForgeFactsAreRefreshedEveryFiveMinutes(t *testing.T) {
	clock := &manualClock{now: time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)}
	forge := connectedAs(4000)
	cache := newForgeCache(clock.Now)
	run := cache.memo(githubRun(openingWith(forge, nil), "https://github.com/octo/herrscher"), "https://github.com/octo/herrscher")
	for _, step := range []struct {
		advance time.Duration
		want    int
	}{
		{advance: 0, want: 1},
		{advance: 4*time.Minute + 59*time.Second, want: 1},
		{advance: time.Second, want: 2},
		{advance: time.Minute, want: 2},
	} {
		clock.now = clock.now.Add(step.advance)
		run(context.Background(), false)
		if forge.connects != step.want {
			t.Fatalf("after %v the forge was asked %d times, want %d", clock.now, forge.connects, step.want)
		}
	}
}

func TestWatchedForgeFactsAreNotCachedForAnotherCheckout(t *testing.T) {
	clock := &manualClock{now: time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)}
	forge := connectedAs(4000)
	cache := newForgeCache(clock.Now)
	open := openingWith(forge, nil)
	cache.memo(githubRun(open, "https://github.com/octo/herrscher"), "https://github.com/octo/herrscher")(context.Background(), false)
	cache.memo(githubRun(open, "https://github.com/octo/other"), "https://github.com/octo/other")(context.Background(), false)
	if forge.connects != 2 {
		t.Errorf("forge asked %d times, want 2 for two checkouts", forge.connects)
	}
}

func TestOpenerFailureIsUnreachable(t *testing.T) {
	report, _ := probeOnce(t, openingWith(nil, errors.New("no plugin")), "")
	if report.State != contracts.HealthDown || report.Detail.Code != contracts.CodeHealthUnreachable {
		t.Errorf("report = %+v, want down and unreachable", report)
	}
}

func TestForgeProbesNeedCredentials(t *testing.T) {
	if probes := forgeProbes(healthInputs{}); probes != nil {
		t.Errorf("probes without credentials = %d, want none", len(probes))
	}
	if probes := forgeProbes(healthInputs{forge: forgeCredentials(nil)}); probes != nil {
		t.Errorf("probes with nil credentials = %d, want none", len(probes))
	}
}

func TestCheckoutOriginIsEmptyOutsideACheckout(t *testing.T) {
	if origin := CheckoutOrigin(context.Background(), t.TempDir()); origin != "" {
		t.Errorf("CheckoutOrigin() = %q, want empty outside a checkout", origin)
	}
}
