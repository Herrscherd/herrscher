package health_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/health"
)

func TestForgeFailureNamesTheCause(t *testing.T) {
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
			report := health.ForgeFailure(tt.err)
			if report.State != tt.want {
				t.Errorf("state = %v, want %v", report.State, tt.want)
			}
			if report.Detail.Code != tt.wantDetail {
				t.Errorf("detail = %v, want %v", report.Detail.Code, tt.wantDetail)
			}
		})
	}
}

func TestForgeFailureRemedyNamesTheConnectCommand(t *testing.T) {
	want := []contracts.Arg{contracts.Text("Command", "herrscher connect github")}
	for _, err := range []error{contracts.ErrForgeNotConnected, contracts.ErrForgeUnauthorized} {
		report := health.ForgeFailure(err)
		if report.Remedy.Code != contracts.CodeHealthRemedyRun || !slices.Equal(report.Remedy.Args, want) {
			t.Errorf("ForgeFailure(%v) remedy = %+v, want the connect command", err, report.Remedy)
		}
	}
}

func TestForgeFailureRateLimitHasNoRemedy(t *testing.T) {
	report := health.ForgeFailure(&contracts.ForgeRateLimitedError{})
	if report.Remedy.Code != 0 {
		t.Errorf("remedy = %+v, want none: a new connection does not lift the limit", report.Remedy)
	}
}

func TestWithRepoAccessDegradesOnlyAHealthyForge(t *testing.T) {
	failure := errors.New("404 not found")
	tests := []struct {
		name   string
		report contracts.ProbeReport
		want   contracts.Code
	}{
		{name: "healthy forge loses repo access", report: health.ForgeReport(contracts.ForgeConnection{Login: "octo"}), want: contracts.CodeHealthRepoAccessUnreadable},
		{name: "rate limited forge keeps its warning", report: health.ForgeReport(contracts.ForgeConnection{Login: "octo", RateLimit: contracts.ForgeRateLimit{Remaining: 1}}), want: contracts.CodeHealthRateLimited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := health.WithRepoAccess(tt.report, "o/r", failure)
			if report.Detail.Code != tt.want {
				t.Errorf("detail = %v, want %v", report.Detail.Code, tt.want)
			}
		})
	}
}
