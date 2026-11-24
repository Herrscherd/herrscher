package health_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/health"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

func forgeSnapshot(forge *health.Forge) health.Snapshot {
	return forgeSnapshotWith(contracts.ProbeReport{State: contracts.HealthOK}, forge)
}

func forgeSnapshotWith(report contracts.ProbeReport, forge *health.Forge) health.Snapshot {
	return health.Snapshot{Checks: []health.Check{{
		Group:  health.GroupForge,
		Name:   "github",
		Report: report,
		Facts:  health.Facts{Forge: forge},
	}}}
}

func tableIn(t *testing.T, lang i18n.Language, snapshot health.Snapshot) string {
	t.Helper()
	var out bytes.Buffer
	if err := snapshot.Table(&out, clitext.NewMessages(), lang, termpaint.Palette{}); err != nil {
		t.Fatalf("Table() error = %v", err)
	}
	return out.String()
}

func tableOf(t *testing.T, snapshot health.Snapshot) string {
	t.Helper()
	return tableIn(t, i18n.English, snapshot)
}

func permission(p contracts.RepoPermission) *contracts.RepoPermission {
	return &p
}

func TestForgeFactsShowTokenSourceQuotaAndRepoPermission(t *testing.T) {
	out := tableOf(t, forgeSnapshot(&health.Forge{
		Source:     contracts.TokenSourceSaved,
		Quota:      &health.Quota{Remaining: 4800},
		Repo:       "octo/herrscher",
		Permission: permission(contracts.PermissionWrite),
	}))
	for _, want := range []string{"token source: saved", "4800 requests left", "octo/herrscher: write"} {
		if !strings.Contains(out, want) {
			t.Errorf("Table() output does not contain %q:\n%s", want, out)
		}
	}
}

func TestForgeFactsAreTranslatedInFrench(t *testing.T) {
	out := tableIn(t, i18n.French, forgeSnapshot(&health.Forge{
		Source:     contracts.TokenSourceSaved,
		Quota:      &health.Quota{Remaining: 4800},
		Repo:       "octo/herrscher",
		Permission: permission(contracts.PermissionWrite),
	}))
	for _, want := range []string{"origine du jeton : enregistré", "4800 requêtes restantes", "octo/herrscher : écriture"} {
		if !strings.Contains(out, want) {
			t.Errorf("Table() output does not contain %q:\n%s", want, out)
		}
	}
	for _, english := range []string{"saved", "write"} {
		if strings.Contains(out, english) {
			t.Errorf("French Table() output carries the English word %q:\n%s", english, out)
		}
	}
}

func TestUnreadableRepoAccessShowsItsReason(t *testing.T) {
	report := contracts.ProbeReport{State: contracts.HealthDegraded, Detail: contracts.Notice{Code: contracts.CodeHealthUnreachable, Args: []contracts.Arg{contracts.Text("Reason", "boom")}}}
	out := tableOf(t, forgeSnapshotWith(report, &health.Forge{Source: contracts.TokenSourceSaved, Repo: "octo/herrscher", RepoErr: "boom"}))
	if !strings.Contains(out, "repository access could not be read: boom") {
		t.Errorf("Table() output does not show the repo access reason:\n%s", out)
	}
}

func TestUnreadableRepoAccessIsTranslatedInFrench(t *testing.T) {
	report := contracts.ProbeReport{State: contracts.HealthDegraded, Detail: contracts.Notice{Code: contracts.CodeHealthUnreachable, Args: []contracts.Arg{contracts.Text("Reason", "boom")}}}
	out := tableIn(t, i18n.French, forgeSnapshotWith(report, &health.Forge{Source: contracts.TokenSourceSaved, Repo: "octo/herrscher", RepoErr: "boom"}))
	if !strings.Contains(out, "l’accès au dépôt n’a pas pu être lu : boom") {
		t.Errorf("French Table() output does not show the repo access reason:\n%s", out)
	}
	if strings.Contains(out, "repository access") {
		t.Errorf("French Table() output carries the English repo access text:\n%s", out)
	}
}

func TestRepoAccessReasonIsAppendedToADegradedDetail(t *testing.T) {
	report := contracts.ProbeReport{State: contracts.HealthDegraded, Detail: contracts.Notice{Code: contracts.CodeHealthRateLimited}}
	plain := tableOf(t, forgeSnapshotWith(report, &health.Forge{Source: contracts.TokenSourceSaved, Login: "octo", Quota: &health.Quota{Remaining: 12}}))
	out := tableOf(t, forgeSnapshotWith(report, &health.Forge{Source: contracts.TokenSourceSaved, Login: "octo", Quota: &health.Quota{Remaining: 12}, RepoErr: "boom"}))
	if strings.Contains(plain, "boom") {
		t.Fatalf("Table() without a repo error shows a reason:\n%s", plain)
	}
	if !strings.Contains(out, "repository access could not be read: boom") {
		t.Errorf("Table() output drops the repo access reason under a degraded report:\n%s", out)
	}
	if !strings.Contains(out, "octo") {
		t.Errorf("Table() output drops the login under a degraded report:\n%s", out)
	}
}

func TestForgeFactsSayThePermissionIsUnknownWhenItCouldNotBeRead(t *testing.T) {
	out := tableOf(t, forgeSnapshot(&health.Forge{Source: contracts.TokenSourceSaved, Repo: "octo/herrscher"}))
	if !strings.Contains(out, "octo/herrscher: permission unknown") {
		t.Errorf("Table() output does not say the permission is unknown:\n%s", out)
	}
}

func TestForgeFactsOmitTheRepoWhenThereIsNone(t *testing.T) {
	out := tableOf(t, forgeSnapshot(&health.Forge{Source: contracts.TokenSourceSaved, Quota: &health.Quota{Remaining: 10}}))
	if strings.Contains(out, "permission") || strings.Contains(out, "/") {
		t.Errorf("Table() output names a repository without one:\n%s", out)
	}
}

func TestForgeFactsOmitTheQuotaWhenTheForgeReportedNone(t *testing.T) {
	out := tableOf(t, forgeSnapshot(&health.Forge{Source: contracts.TokenSourceSaved}))
	if strings.Contains(out, "requests left") || strings.Contains(out, "0 requests") {
		t.Errorf("Table() output reports a quota the forge never sent:\n%s", out)
	}
}

func TestLowQuotaKeepsTheLoginBesideTheWarning(t *testing.T) {
	forge := &health.Forge{Source: contracts.TokenSourceSaved, Login: "octo", Quota: &health.Quota{Remaining: 12}}
	report := contracts.ProbeReport{State: contracts.HealthDegraded, Detail: contracts.Notice{Code: contracts.CodeHealthRateLimited}}
	out := tableOf(t, forgeSnapshotWith(report, forge))
	if !strings.Contains(out, "octo") {
		t.Errorf("Table() output drops the login under a rate limit warning:\n%s", out)
	}
}

func TestForgeFactsSurviveTheWire(t *testing.T) {
	reset := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	raw, err := health.Encode(forgeSnapshot(&health.Forge{
		Source:     contracts.TokenSourceApp,
		Login:      "octo",
		Quota:      &health.Quota{Remaining: 42, Reset: reset},
		Repo:       "octo/herrscher",
		Permission: permission(contracts.PermissionAdmin),
	}))
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := health.Decode(raw)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	got := decoded.Checks[0].Facts.Forge
	if got == nil {
		t.Fatalf("Decode() lost the forge facts:\n%s", raw)
	}
	if got.Source != contracts.TokenSourceApp || got.Login != "octo" || got.Quota == nil || got.Quota.Remaining != 42 || !got.Quota.Reset.Equal(reset) || got.Repo != "octo/herrscher" || got.Permission == nil || *got.Permission != contracts.PermissionAdmin {
		t.Errorf("decoded forge facts = %+v", *got)
	}
}

func TestForgeFactsWithoutQuotaSurviveTheWire(t *testing.T) {
	raw, err := health.Encode(forgeSnapshot(&health.Forge{Source: contracts.TokenSourceCLI}))
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := health.Decode(raw)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got := decoded.Checks[0].Facts.Forge; got == nil || got.Quota != nil {
		t.Errorf("decoded forge facts = %+v, want no quota", got)
	}
}

func TestForgeGroupIsTheLastOneAndParses(t *testing.T) {
	groups := health.Groups()
	if groups[len(groups)-1] != health.GroupForge {
		t.Errorf("last group = %v, want %v", groups[len(groups)-1], health.GroupForge)
	}
	parsed, err := health.ParseGroup("forge")
	if err != nil || parsed != health.GroupForge {
		t.Errorf("ParseGroup(forge) = %v, %v", parsed, err)
	}
}

func TestRunnerKeepsTheFactsAProbeReturns(t *testing.T) {
	run := func(context.Context, bool) (contracts.ProbeReport, health.Facts) {
		return contracts.ProbeReport{State: contracts.HealthOK}, health.Facts{Forge: &health.Forge{Source: contracts.TokenSourceEnv}}
	}
	checks, _ := health.Runner{}.Run(context.Background(), []health.Probe{{Check: health.Check{Group: health.GroupForge, Name: "github"}, Run: run}}, false)
	if checks[0].Facts.Forge == nil || checks[0].Facts.Forge.Source != contracts.TokenSourceEnv {
		t.Fatalf("check facts = %+v, want the forge facts the probe returned", checks[0].Facts)
	}
}
