package manager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
)

const sessionProfileMCP = `{"mcpServers": {"alpha": {"command": "alpha-bin"}, "beta": {"command": "beta-bin"}}}`

func profiledSessionHandler(t *testing.T, profiles string) (*Handler, *fakeWT) {
	t.Helper()
	h, _, _, wt, _, _ := newTestHandler(t, "")
	store := agent.NewStore(t.TempDir())
	created, err := store.Create(agent.CreateSpec{Name: "roblox", Soul: "PERSONA", MCP: "unused"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.Home, "mcp.json"), []byte(sessionProfileMCP), 0o600); err != nil {
		t.Fatal(err)
	}
	if profiles != "" {
		if err := os.WriteFile(filepath.Join(created.Home, "mcp-profiles.json"), []byte(profiles), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h.agents = store
	return h, wt
}

func provisionChild(t *testing.T, h *Handler, wt *fakeWT, parent string) (string, agent.MCPSelection, error) {
	t.Helper()
	dir := t.TempDir()
	space := workspace{path: dir}
	req := &createRequest{
		name:    "child",
		runtime: runtimeChoice{agent: "roblox"},
		options: sessionOptions{parent: parent},
		mode:    approval.ModeBypass.String(),
	}
	selection, err := h.provisionAgent(t.Context(), req, wt, "repo", &space)
	return dir, selection, err
}

func mcpServerNames(t *testing.T, dir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	servers, err := agent.ParseMCPServers(raw)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Name)
	}
	return names
}

func TestProvisionWorkerKeepsOnlyTheWorkerServers(t *testing.T) {
	h, wt := profiledSessionHandler(t, `{"worker": ["alpha"]}`)
	dir, selection, err := provisionChild(t, h, wt, "lead-session")
	if err != nil {
		t.Fatal(err)
	}
	if got := mcpServerNames(t, dir); !slices.Equal(got, []string{"alpha"}) {
		t.Fatalf("worker .mcp.json holds %v, want alpha only", got)
	}
	if !selection.Applied || selection.Role != agent.RoleWorker || !slices.Equal(selection.Dropped, []string{"beta"}) {
		t.Fatalf("selection = %+v, want the worker profile applied", selection)
	}
}

func TestProvisionTopLevelSessionKeepsEveryServer(t *testing.T) {
	h, wt := profiledSessionHandler(t, `{"worker": ["alpha"]}`)
	dir, selection, err := provisionChild(t, h, wt, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := mcpServerNames(t, dir); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf("top level .mcp.json holds %v, want every server", got)
	}
	if selection.Applied {
		t.Fatalf("a top level session has no role: %+v", selection)
	}
}

func TestProvisionUnknownProfileServerRollsBackWithLocalizedError(t *testing.T) {
	cases := []struct {
		name string
		lang i18n.Language
		want string
	}{
		{name: "english", lang: i18n.English, want: `names server "ghost", which its mcp.json does not declare`},
		{name: "french", lang: i18n.French, want: `nomme le serveur « ghost », que son mcp.json ne déclare pas`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, wt := profiledSessionHandler(t, `{"worker": ["ghost"]}`)
			h.SetLanguage(tc.lang)
			dir, _, err := provisionChild(t, h, wt, "lead-session")
			if err == nil {
				t.Fatal("provisioning must fail on an unknown profile server")
			}
			requireLocalizedDetail(t, err, tc.want)
			if _, statErr := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(statErr) {
				t.Fatalf(".mcp.json written despite the error: %v", statErr)
			}
			if !slices.Contains(wt.removed, "child") {
				t.Fatalf("worktree not rolled back, removed = %v", wt.removed)
			}
		})
	}
}

func requireLocalizedDetail(t *testing.T, err error, want string) {
	t.Helper()
	var localized *localizedError
	if !errors.As(err, &localized) {
		t.Fatalf("error is %T, want a localizedError", err)
	}
	if got := fmt.Sprint(localized.Notice().Args[0].Value()); !strings.Contains(got, want) {
		t.Fatalf("localized detail = %q, want it to contain %q", got, want)
	}
}

func TestProvisionRejectsAProfileRoleThatIsNotAssignable(t *testing.T) {
	h, wt := profiledSessionHandler(t, `{"lead": ["alpha"]}`)
	h.SetLanguage(i18n.English)
	_, _, err := provisionChild(t, h, wt, "lead-session")
	if err == nil || !strings.Contains(err.Error(), `"lead"`) || !strings.Contains(err.Error(), "worker") {
		t.Fatalf("error = %v, want the key and the accepted roles", err)
	}
}

func TestMCPProfileLineNamesKeptAndDroppedInBothLanguages(t *testing.T) {
	h, _ := profiledSessionHandler(t, "")
	selection := agent.MCPSelection{Role: agent.RoleWorker, Applied: true, Kept: []string{"alpha"}, Dropped: []string{"beta"}}
	cases := []struct {
		lang i18n.Language
		want string
	}{
		{lang: i18n.English, want: "Profile of role worker: loaded alpha, left out beta."},
		{lang: i18n.French, want: "Profil du rôle worker : chargés alpha, écartés beta."},
	}
	for _, tc := range cases {
		if got := h.mcpProfileLine(tc.lang, selection); !strings.Contains(got, tc.want) {
			t.Fatalf("line = %q, want %q", got, tc.want)
		}
	}
	if got := h.mcpProfileLine(i18n.English, agent.MCPSelection{Role: agent.RoleWorker}); got != "" {
		t.Fatalf("a session without applied profile has a line: %q", got)
	}
}
