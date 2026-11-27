package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const profilesMCPJSON = `{
  "mcpServers": {
    "alpha": {"command": "alpha-bin", "args": ["serve"]},
    "beta": {"command": "beta-bin", "env": {"BETA_TOKEN": "secret-beta"}},
    "gamma": {"command": "gamma-bin"}
  }
}`

func profiledAgent(t *testing.T, profiles string) Agent {
	t.Helper()
	a, err := NewStore(t.TempDir()).Create(CreateSpec{Name: "roblox", Soul: "PERSONA", MCP: "neublox serve --project {{WORKTREE}}"})
	if err != nil {
		t.Fatal(err)
	}
	writeAgentFile(t, a, "mcp.json", profilesMCPJSON)
	if profiles != "" {
		writeAgentFile(t, a, mcpProfilesFile, profiles)
	}
	return a
}

func writeAgentFile(t *testing.T, a Agent, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(a.Home, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func serverNames(t *testing.T, document []byte) []string {
	t.Helper()
	servers, err := ParseMCPServers(document)
	if err != nil {
		t.Fatalf("selected document does not parse: %v\n%s", err, document)
	}
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Name)
	}
	return names
}

func TestSelectMCPKeepsOnlyTheRoleServers(t *testing.T) {
	a := profiledAgent(t, `{"worker": ["alpha", "beta"]}`)
	selection, err := a.SelectMCP(RoleWorker)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.Applied || selection.Role != RoleWorker {
		t.Fatalf("profile not applied: %+v", selection)
	}
	if !slices.Equal(selection.Kept, []string{"alpha", "beta"}) || !slices.Equal(selection.Dropped, []string{"gamma"}) {
		t.Fatalf("kept %v, dropped %v", selection.Kept, selection.Dropped)
	}
	if got := serverNames(t, selection.Document); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf("document holds %v, want alpha and beta", got)
	}
}

func TestSelectMCPPreservesRawServerEntries(t *testing.T) {
	a := profiledAgent(t, `{"worker": ["beta"]}`)
	selection, err := a.SelectMCP(RoleWorker)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(selection.Document), `"BETA_TOKEN": "secret-beta"`) {
		t.Fatalf("env of the kept server lost:\n%s", selection.Document)
	}
	if !json.Valid(selection.Document) {
		t.Fatalf("selected document is not valid JSON:\n%s", selection.Document)
	}
}

func TestSelectMCPWithoutProfileKeepsEveryServer(t *testing.T) {
	a := profiledAgent(t, "")
	raw, err := os.ReadFile(filepath.Join(a.Home, mcpFile))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := a.SelectMCP(RoleWorker)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Applied || selection.Kept != nil || selection.Dropped != nil {
		t.Fatalf("no profile must not be applied: %+v", selection)
	}
	if !bytes.Equal(selection.Document, raw) {
		t.Fatalf("document changed without a profile:\n%s", selection.Document)
	}
}

func TestSelectMCPUnmatchedRoleKeepsEveryServer(t *testing.T) {
	a := profiledAgent(t, `{"worker": ["alpha"]}`)
	raw, err := os.ReadFile(filepath.Join(a.Home, mcpFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"", "lead", "reviewer"} {
		selection, err := a.SelectMCP(role)
		if err != nil {
			t.Fatalf("role %q: %v", role, err)
		}
		if selection.Applied || !bytes.Equal(selection.Document, raw) {
			t.Fatalf("role %q changed the document: %+v", role, selection)
		}
	}
}

func TestSelectMCPUnknownServerIsAnError(t *testing.T) {
	a := profiledAgent(t, `{"worker": ["alpha", "ghost"]}`)
	_, err := a.SelectMCP(RoleWorker)
	profileErr, ok := errors.AsType[*UnknownProfileServerError](err)
	if !ok {
		t.Fatalf("want UnknownProfileServerError, got %v", err)
	}
	if profileErr.Role != RoleWorker || profileErr.Server != "ghost" {
		t.Fatalf("error names role %q server %q", profileErr.Role, profileErr.Server)
	}
	if !strings.Contains(err.Error(), mcpProfilesFile) || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("error text does not name the file and server: %v", err)
	}
}

func TestSelectMCPMalformedProfileIsAnError(t *testing.T) {
	a := profiledAgent(t, `{"worker": `)
	if _, err := a.SelectMCP(RoleWorker); err == nil {
		t.Fatal("malformed mcp-profiles.json must fail")
	}
}

func TestMaterializeAppliesTheRoleProfileToBothFiles(t *testing.T) {
	a := profiledAgent(t, `{"worker": ["alpha", "beta"]}`)
	a.Role = RoleWorker
	wt := t.TempDir()
	if err := a.Materialize(t.Context(), wt); err != nil {
		t.Fatal(err)
	}
	mcp, err := os.ReadFile(filepath.Join(wt, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := serverNames(t, mcp); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf(".mcp.json holds %v", got)
	}
	codex, err := os.ReadFile(filepath.Join(wt, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(codex), "[mcp_servers.alpha]") || !strings.Contains(string(codex), "[mcp_servers.beta]") {
		t.Fatalf("codex config misses a kept server:\n%s", codex)
	}
	if strings.Contains(string(codex), "gamma") || strings.Contains(string(mcp), "gamma") {
		t.Fatalf("a dropped server leaked into the session:\n.mcp.json\n%s\ncodex\n%s", mcp, codex)
	}
}

func TestMaterializeWithoutRoleKeepsEveryServerInBothFiles(t *testing.T) {
	a := profiledAgent(t, `{"worker": ["alpha"]}`)
	wt := t.TempDir()
	if err := a.Materialize(t.Context(), wt); err != nil {
		t.Fatal(err)
	}
	mcp, err := os.ReadFile(filepath.Join(wt, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := serverNames(t, mcp); !slices.Equal(got, []string{"alpha", "beta", "gamma"}) {
		t.Fatalf(".mcp.json holds %v without a role", got)
	}
	codex, err := os.ReadFile(filepath.Join(wt, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(string(codex), "[mcp_servers."+name+"]") {
			t.Fatalf("codex config misses %s without a role:\n%s", name, codex)
		}
	}
}

func TestMaterializeUnknownProfileServerWritesNothing(t *testing.T) {
	a := profiledAgent(t, `{"worker": ["ghost"]}`)
	a.Role = RoleWorker
	wt := t.TempDir()
	err := a.Materialize(t.Context(), wt)
	if _, ok := errors.AsType[*UnknownProfileServerError](err); !ok {
		t.Fatalf("want UnknownProfileServerError, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(wt, ".mcp.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf(".mcp.json written despite the error: %v", statErr)
	}
}
