package host

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/mcpweight"
)

const profileHostMCP = `{"mcpServers":{"keep":{"command":"/nonexistent/keep"},"drop":{"command":"/nonexistent/drop"}}}`

func profiledWeightHome(t *testing.T, profiles string) string {
	t.Helper()
	home := writeMCPHome(t, profileHostMCP)
	if profiles != "" {
		if err := os.WriteFile(filepath.Join(home, "mcp-profiles.json"), []byte(profiles), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestMCPWeightRoleDescribesOnlyTheKeptServers(t *testing.T) {
	agents := singleAgent{name: "roled", home: profiledWeightHome(t, `{"worker": ["keep"]}`)}
	out, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "roled", role: "worker", asJSON: true})
	if err != nil {
		t.Fatal(err)
	}
	var results []mcpweight.Result
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, out)
	}
	if len(results) != 1 || results[0].Name != "keep" {
		t.Fatalf("results = %+v, want only the kept server", results)
	}
}

func TestMCPWeightRoleTextReportNamesKeptAndDroppedServers(t *testing.T) {
	agents := singleAgent{name: "roled", home: profiledWeightHome(t, `{"worker": ["keep"]}`)}
	out, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "roled", role: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	want := "Profile of role worker: loaded keep, left out drop."
	if !strings.Contains(out, want) {
		t.Fatalf("text report misses %q:\n%s", want, out)
	}
}

func TestMCPWeightRoleWithoutProfileSaysEveryServerIsLoaded(t *testing.T) {
	agents := singleAgent{name: "roled", home: profiledWeightHome(t, "")}
	out, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "roled", role: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	want := "No profile for role worker: every server is loaded."
	if !strings.Contains(out, want) {
		t.Fatalf("text report misses %q:\n%s", want, out)
	}
	if !strings.Contains(out, "keep") || !strings.Contains(out, "drop") {
		t.Fatalf("a role without profile must measure every server:\n%s", out)
	}
}

func TestMCPWeightWithoutRoleHasNoProfileNote(t *testing.T) {
	agents := singleAgent{name: "roled", home: profiledWeightHome(t, `{"worker": ["keep"]}`)}
	out, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "roled"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Profile of role") || strings.Contains(out, "No profile for role") {
		t.Fatalf("a report without role must not talk about profiles:\n%s", out)
	}
}

func TestMCPWeightUnknownProfileServerIsAnError(t *testing.T) {
	agents := singleAgent{name: "roled", home: profiledWeightHome(t, `{"worker": ["ghost"]}`)}
	_, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "roled", role: "worker"})
	if _, ok := errors.AsType[*agent.UnknownProfileServerError](err); !ok {
		t.Fatalf("error = %v, want the cause UnknownProfileServerError kept reachable", err)
	}
}

func TestMCPWeightUnknownProfileServerIsLocalized(t *testing.T) {
	cases := []struct {
		name string
		lang i18n.Language
		want string
	}{
		{name: "english", lang: i18n.English, want: `the MCP profile of role "worker" of agent "roled" names server "ghost", which its mcp.json does not declare`},
		{name: "french", lang: i18n.French, want: `le profil MCP du rôle « worker » de l'agent « roled » nomme le serveur « ghost », que son mcp.json ne déclare pas`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agents := singleAgent{name: "roled", home: profiledWeightHome(t, `{"worker": ["ghost"]}`)}
			_, err := mcpWeightReport(context.Background(), clitext.NewMessages(), fixedLanguage(tc.lang)(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "roled", role: "worker"})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if _, ok := errors.AsType[*agent.UnknownProfileServerError](err); !ok {
				t.Fatalf("localized error lost its cause: %v", err)
			}
		})
	}
}

func TestMCPWeightRoleNotAssignableIsLocalized(t *testing.T) {
	cases := []struct {
		name string
		lang i18n.Language
		want string
	}{
		{name: "english", lang: i18n.English, want: `role not assignable: "lead" (accepted roles: worker)`},
		{name: "french", lang: i18n.French, want: `rôle non assignable : « lead » (rôles acceptés : worker)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agents := singleAgent{name: "roled", home: profiledWeightHome(t, `{"worker": ["keep"]}`)}
			_, err := mcpWeightReport(context.Background(), clitext.NewMessages(), fixedLanguage(tc.lang)(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "roled", role: "lead"})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if _, ok := errors.AsType[*agent.UnassignableRoleError](err); !ok {
				t.Fatalf("localized error lost its cause: %v", err)
			}
		})
	}
}

func TestMCPWeightRoleKeyNotAssignableInProfileIsAnError(t *testing.T) {
	agents := singleAgent{name: "roled", home: profiledWeightHome(t, `{"lead": ["keep"]}`)}
	_, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "roled", role: "worker"})
	if _, ok := errors.AsType[*agent.UnassignableRoleError](err); !ok {
		t.Fatalf("error = %v, want the profile key refused", err)
	}
}

func TestMCPWeightNullProfileEntryIsAnError(t *testing.T) {
	agents := singleAgent{name: "roled", home: profiledWeightHome(t, `{"worker": null}`)}
	_, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "roled", role: "worker"})
	if _, ok := errors.AsType[*agent.NullProfileEntryError](err); !ok {
		t.Fatalf("error = %v, want the null entry refused", err)
	}
}

func TestMCPWeightRoleWithoutMCPJSONIsAsWithoutRole(t *testing.T) {
	agents := singleAgent{name: "roled", home: t.TempDir()}
	withRole, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "roled", role: "worker", asJSON: true})
	if err != nil {
		t.Fatalf("a role without mcp.json must not fail: %v", err)
	}
	withoutRole, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "roled", asJSON: true})
	if err != nil {
		t.Fatal(err)
	}
	if withRole != withoutRole || withRole != "[]" {
		t.Fatalf("role report %q, without role %q, want both to be the empty list", withRole, withoutRole)
	}
}

func TestMCPWeightDeepWithRoleMeasuresOnlyTheKeptServers(t *testing.T) {
	agents := singleAgent{name: "roled", home: profiledWeightHome(t, `{"worker": ["keep"]}`)}
	out, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "roled", role: "worker", deep: true, asJSON: true})
	if err != nil {
		t.Fatal(err)
	}
	var results []mcpweight.Result
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, out)
	}
	if len(results) != 1 || results[0].Name != "keep" {
		t.Fatalf("deep results = %+v, want only the kept server", results)
	}
}
