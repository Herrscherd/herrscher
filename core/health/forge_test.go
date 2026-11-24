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
	return health.Snapshot{Checks: []health.Check{{
		Group:  health.GroupForge,
		Name:   "github",
		Report: contracts.ProbeReport{State: contracts.HealthOK},
		Facts:  health.Facts{Forge: forge},
	}}}
}

func tableOf(t *testing.T, snapshot health.Snapshot) string {
	t.Helper()
	var out bytes.Buffer
	if err := snapshot.Table(&out, clitext.NewMessages(), i18n.English, termpaint.Palette{}); err != nil {
		t.Fatalf("Table() error = %v", err)
	}
	return out.String()
}

func TestForgeFactsShowTokenSourceQuotaAndRepoPermission(t *testing.T) {
	out := tableOf(t, forgeSnapshot(&health.Forge{
		Source:     contracts.TokenSourceSaved,
		Remaining:  4800,
		Repo:       "octo/herrscher",
		Permission: "write",
	}))
	for _, want := range []string{"token from saved", "4800 requests left", "octo/herrscher: write"} {
		if !strings.Contains(out, want) {
			t.Errorf("Table() output does not contain %q:\n%s", want, out)
		}
	}
}

func TestForgeFactsSayThePermissionIsUnknownWhenItCouldNotBeRead(t *testing.T) {
	out := tableOf(t, forgeSnapshot(&health.Forge{Source: contracts.TokenSourceSaved, Repo: "octo/herrscher"}))
	if !strings.Contains(out, "octo/herrscher: permission unknown") {
		t.Errorf("Table() output does not say the permission is unknown:\n%s", out)
	}
}

func TestForgeFactsOmitTheRepoWhenThereIsNone(t *testing.T) {
	out := tableOf(t, forgeSnapshot(&health.Forge{Source: contracts.TokenSourceSaved, Remaining: 10}))
	if strings.Contains(out, "permission") || strings.Contains(out, "/") {
		t.Errorf("Table() output names a repository without one:\n%s", out)
	}
}

func TestForgeFactsSurviveTheWire(t *testing.T) {
	reset := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	raw, err := health.Encode(forgeSnapshot(&health.Forge{Source: contracts.TokenSourceApp, Remaining: 42, Reset: reset, Repo: "octo/herrscher", Permission: "admin"}))
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
	if got.Source != contracts.TokenSourceApp || got.Remaining != 42 || !got.Reset.Equal(reset) || got.Repo != "octo/herrscher" || got.Permission != "admin" {
		t.Errorf("decoded forge facts = %+v", *got)
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

func TestWatchedFactsReachTheCheck(t *testing.T) {
	watch := func(context.Context, bool) (contracts.ProbeReport, health.Facts) {
		return contracts.ProbeReport{State: contracts.HealthOK}, health.Facts{Forge: &health.Forge{Remaining: 7}}
	}
	checks, _ := health.Runner{}.Run(context.Background(), []health.Probe{{Check: health.Check{Group: health.GroupForge, Name: "github"}, Watch: watch}}, false)
	if checks[0].Facts.Forge == nil || checks[0].Facts.Forge.Remaining != 7 {
		t.Fatalf("check facts = %+v, want the watched forge facts", checks[0].Facts)
	}
}
