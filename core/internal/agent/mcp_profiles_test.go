package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestSelectMCPRoleWithoutEntryKeepsEveryServer(t *testing.T) {
	a := profiledAgent(t, `{}`)
	raw, err := os.ReadFile(filepath.Join(a.Home, mcpFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"", RoleWorker} {
		selection, err := a.SelectMCP(role)
		if err != nil {
			t.Fatalf("role %q: %v", role, err)
		}
		if selection.Applied || !bytes.Equal(selection.Document, raw) {
			t.Fatalf("role %q changed the document: %+v", role, selection)
		}
	}
}

func TestSelectMCPRejectsRolesTheProvisioningDoesNotAssign(t *testing.T) {
	for _, role := range []string{"lead", "Worker", "workers", "reviewer"} {
		t.Run("argument "+role, func(t *testing.T) {
			a := profiledAgent(t, `{"worker": ["alpha"]}`)
			_, err := a.SelectMCP(role)
			roleErr, ok := errors.AsType[*UnassignableRoleError](err)
			if !ok || roleErr.Role != role || roleErr.InProfile {
				t.Fatalf("SelectMCP(%q) = %v, want UnassignableRoleError for the role", role, err)
			}
		})
	}
}

func TestSelectMCPRejectsProfileKeysThatAreNotAssignableRoles(t *testing.T) {
	for _, key := range []string{"lead", "Worker", "workers", "typo"} {
		t.Run(key, func(t *testing.T) {
			a := profiledAgent(t, `{"worker": ["alpha"], "`+key+`": ["beta"]}`)
			_, err := a.SelectMCP(RoleWorker)
			roleErr, ok := errors.AsType[*UnassignableRoleError](err)
			if !ok || roleErr.Role != key || !roleErr.InProfile {
				t.Fatalf("SelectMCP = %v, want UnassignableRoleError naming %q from the profile", err, key)
			}
			if !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), RoleWorker) {
				t.Fatalf("error does not name the key and the accepted roles: %v", err)
			}
		})
	}
}

func TestSelectMCPBadProfileKeyFailsSessionsWithoutRoleToo(t *testing.T) {
	a := profiledAgent(t, `{"lead": ["alpha"]}`)
	if _, err := a.SelectMCP(""); err == nil {
		t.Fatal("a profile with a key that is not assignable must fail every session")
	}
}

func TestSelectMCPNullProfileEntryIsAnError(t *testing.T) {
	a := profiledAgent(t, `{"worker": null}`)
	_, err := a.SelectMCP(RoleWorker)
	nullErr, ok := errors.AsType[*NullProfileEntryError](err)
	if !ok || nullErr.Role != RoleWorker {
		t.Fatalf("SelectMCP = %v, want NullProfileEntryError for worker", err)
	}
}

func TestSelectMCPValidatesOnlyTheKeptServers(t *testing.T) {
	a := profiledAgent(t, `{"worker": ["alpha"]}`)
	writeAgentFile(t, a, "mcp.json", `{"mcpServers": {"alpha": {"command": "alpha-bin"}, "broken": {"type": "stdio"}}}`)
	selection, err := a.SelectMCP(RoleWorker)
	if err != nil {
		t.Fatalf("a dropped server without command must not block the session: %v", err)
	}
	if !slices.Equal(selection.Kept, []string{"alpha"}) || !slices.Equal(selection.Dropped, []string{"broken"}) {
		t.Fatalf("kept %v, dropped %v", selection.Kept, selection.Dropped)
	}
}

func TestSelectMCPKeepsTheOtherTopLevelKeysWithoutHTMLEscaping(t *testing.T) {
	a := profiledAgent(t, `{"worker": ["alpha"]}`)
	writeAgentFile(t, a, "mcp.json", `{"$schema": "https://example.test/mcp.json", "note": "a<b & c>d", "mcpServers": {"alpha": {"command": "alpha-bin", "args": ["x<y&z"]}, "gamma": {"command": "gamma-bin"}}}`)
	selection, err := a.SelectMCP(RoleWorker)
	if err != nil {
		t.Fatal(err)
	}
	document := string(selection.Document)
	for _, want := range []string{`"$schema": "https://example.test/mcp.json"`, `"note": "a<b & c>d"`, `"x<y&z"`} {
		if !strings.Contains(document, want) {
			t.Fatalf("document lost %s:\n%s", want, document)
		}
	}
	if strings.Contains(document, "gamma") {
		t.Fatalf("dropped server kept:\n%s", document)
	}
}

func TestDelegationRole(t *testing.T) {
	cases := []struct {
		name   string
		parent string
		want   string
	}{
		{name: "top level session has no role", parent: "", want: ""},
		{name: "session spawned by a lead is a worker", parent: "lead-session", want: RoleWorker},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DelegationRole(tc.parent); got != tc.want {
				t.Fatalf("DelegationRole(%q) = %q, want %q", tc.parent, got, tc.want)
			}
		})
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
	runGit(t, wt, "init", "-q")
	excludeBefore := readFileOrEmpty(t, filepath.Join(wt, ".git", "info", "exclude"))
	err := a.Materialize(t.Context(), wt)
	if _, ok := errors.AsType[*UnknownProfileServerError](err); !ok {
		t.Fatalf("want UnknownProfileServerError, got %v", err)
	}
	for _, name := range []string{".mcp.json", ".claude", ".codex", "AGENTS.md"} {
		if _, statErr := os.Stat(filepath.Join(wt, name)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("%s written despite the error: %v", name, statErr)
		}
	}
	if readFileOrEmpty(t, filepath.Join(wt, ".git", "info", "exclude")) != excludeBefore {
		t.Fatal("git excludes written despite the error")
	}
}

func TestMaterializeNullProfileEntryWritesNothing(t *testing.T) {
	a := profiledAgent(t, `{"worker": null}`)
	a.Role = RoleWorker
	wt := t.TempDir()
	if err := a.Materialize(t.Context(), wt); err == nil {
		t.Fatal("a null profile entry must fail the materialization")
	}
	if _, statErr := os.Stat(filepath.Join(wt, ".mcp.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf(".mcp.json written despite the error: %v", statErr)
	}
}

func TestMaterializeWritesServerFilesOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not enforced on Windows")
	}
	a := profiledAgent(t, `{"worker": ["alpha"]}`)
	a.Role = RoleWorker
	wt := t.TempDir()
	if err := a.Materialize(t.Context(), wt); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(wt, ".mcp.json"), filepath.Join(wt, ".codex", "config.toml")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Fatalf("%s mode = %o, want 600", filepath.Base(path), mode)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func readFileOrEmpty(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
