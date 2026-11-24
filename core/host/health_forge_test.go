package host

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/health"
)

type fakeForge struct {
	contracts.Forge

	connection contracts.ForgeConnection
	connErr    error
	permission contracts.RepoPermission
	accessErr  error
	asked      []contracts.ForgeRepoRef
}

func (f *fakeForge) Connection(context.Context) (contracts.ForgeConnection, error) {
	return f.connection, f.connErr
}

func (f *fakeForge) RepoAccess(_ context.Context, repo contracts.ForgeRepoRef) (contracts.RepoPermission, error) {
	f.asked = append(f.asked, repo)
	return f.permission, f.accessErr
}

func openingWith(forge contracts.Forge, err error) forgeOpener {
	return func(context.Context, contracts.ForgeHost) (contracts.Forge, error) {
		return forge, err
	}
}

func watchOnce(t *testing.T, open forgeOpener, origin string) (contracts.ProbeReport, health.Facts) {
	t.Helper()
	report, facts := githubWatch(open, origin)(context.Background(), false)
	return report, facts
}

func connectedAs(remaining int) *fakeForge {
	return &fakeForge{connection: contracts.ForgeConnection{Login: "octo", Source: contracts.TokenSourceSaved, RateLimit: contracts.ForgeRateLimit{Remaining: remaining}}}
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
			report, _ := watchOnce(t, openingWith(&fakeForge{connErr: tt.err}, nil), "")
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
	report, _ := watchOnce(t, openingWith(&fakeForge{connErr: contracts.ErrForgeNotConnected}, nil), "")
	if !sameNotice(report.Remedy, gitHubConnectRemedy()) {
		t.Errorf("remedy = %+v, want the connect command", report.Remedy)
	}
}

func TestConnectedAccountReportsItsLogin(t *testing.T) {
	report, facts := watchOnce(t, openingWith(connectedAs(4000), nil), "")
	want := contracts.Notice{Code: contracts.CodeHealthSignedInAs, Args: []contracts.Arg{contracts.Text("Name", "octo")}}
	if report.State != contracts.HealthOK || !sameNotice(report.Detail, want) {
		t.Fatalf("report = %+v, want ok signed in as octo", report)
	}
	if facts.Forge == nil || facts.Forge.Source != contracts.TokenSourceSaved || facts.Forge.Remaining != 4000 {
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
			report, _ := watchOnce(t, openingWith(connectedAs(tt.remaining), nil), "")
			if report.State != tt.want {
				t.Errorf("state = %v, want %v", report.State, tt.want)
			}
		})
	}
}

func TestNoSessionRepoAsksForNoPermission(t *testing.T) {
	forge := connectedAs(4000)
	_, facts := watchOnce(t, openingWith(forge, nil), "")
	if len(forge.asked) != 0 || facts.Forge.Repo != "" {
		t.Errorf("asked %v and reported repo %q, want neither", forge.asked, facts.Forge.Repo)
	}
}

func TestNonGitHubOriginIsSkipped(t *testing.T) {
	forge := connectedAs(4000)
	_, facts := watchOnce(t, openingWith(forge, nil), "https://gitlab.com/octo/herrscher.git")
	if len(forge.asked) != 0 || facts.Forge.Repo != "" {
		t.Errorf("asked %v and reported repo %q, want neither", forge.asked, facts.Forge.Repo)
	}
}

func TestSessionRepoReportsThePermissionOnGitHub(t *testing.T) {
	forge := connectedAs(4000)
	forge.permission = contracts.PermissionWrite
	_, facts := watchOnce(t, openingWith(forge, nil), "git@github.com:octo/herrscher.git")
	if len(forge.asked) != 1 {
		t.Fatalf("RepoAccess asked %d times, want once", len(forge.asked))
	}
	if facts.Forge.Repo != "octo/herrscher" || facts.Forge.Permission != contracts.PermissionWrite.String() {
		t.Errorf("repo = %q permission = %q, want octo/herrscher write", facts.Forge.Repo, facts.Forge.Permission)
	}
}

func TestUnreadableRepoPermissionKeepsTheRepoAndDropsTheRight(t *testing.T) {
	forge := connectedAs(4000)
	forge.accessErr = contracts.ErrForgeNotFound
	report, facts := watchOnce(t, openingWith(forge, nil), "https://github.com/octo/herrscher")
	if report.State != contracts.HealthOK {
		t.Errorf("state = %v, want ok: an unreadable right is not a fault of the connection", report.State)
	}
	if facts.Forge.Repo != "octo/herrscher" || facts.Forge.Permission != "" {
		t.Errorf("repo = %q permission = %q, want octo/herrscher with no permission", facts.Forge.Repo, facts.Forge.Permission)
	}
}

func TestOpenerFailureIsUnreachable(t *testing.T) {
	report, _ := watchOnce(t, openingWith(nil, errors.New("no plugin")), "")
	if report.State != contracts.HealthDown || report.Detail.Code != contracts.CodeHealthUnreachable {
		t.Errorf("report = %+v, want down and unreachable", report)
	}
}

func TestForgeProbesNeedCredentialsAndABuiltGitHubPlugin(t *testing.T) {
	if probes := forgeProbes(healthInputs{}); probes != nil {
		t.Errorf("probes without credentials = %d, want none", len(probes))
	}
	if probes := forgeProbes(healthInputs{forge: forgeCredentials(nil)}); probes != nil {
		t.Errorf("probes with nil credentials = %d, want none", len(probes))
	}
}

func TestSessionOriginIsEmptyOutsideACheckout(t *testing.T) {
	if origin := SessionOrigin(context.Background(), t.TempDir()); origin != "" {
		t.Errorf("SessionOrigin() = %q, want empty outside a checkout", origin)
	}
}
