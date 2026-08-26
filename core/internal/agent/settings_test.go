package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func settingsMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("settings are not valid json: %v", err)
	}
	return m
}

func TestInjectApprovalHook(t *testing.T) {
	base, err := buildSettings("neublox")
	if err != nil {
		t.Fatalf("buildSettings: %v", err)
	}
	out, err := injectApprovalHook(base, "/far/bin/herrscher")
	if err != nil {
		t.Fatalf("injectApprovalHook: %v", err)
	}
	m := settingsMap(t, out)
	if !strings.Contains(string(out), "'/far/bin/herrscher' approve hook") {
		t.Fatalf("the binary is not in the settings:\n%s", out)
	}
	perms, _ := m["permissions"].(map[string]any)
	if perms["defaultMode"] != "acceptEdits" {
		t.Fatalf("the existing settings were not left alone:\n%s", out)
	}
	if m["enableAllProjectMcpServers"] != true {
		t.Fatalf("the existing settings were not left alone:\n%s", out)
	}
}

func TestInjectApprovalHookKeepsHooksAlreadyThere(t *testing.T) {
	base := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"mine"}]}]}}`)
	out, err := injectApprovalHook(base, "/bin/h")
	if err != nil {
		t.Fatalf("injectApprovalHook: %v", err)
	}
	if !strings.Contains(string(out), `"mine"`) {
		t.Fatalf("an operator's own hook was dropped:\n%s", out)
	}
	hooks := settingsMap(t, out)["hooks"].(map[string]any)
	if n := len(hooks["PreToolUse"].([]any)); n != 2 {
		t.Fatalf("got %d PreToolUse entries, want 2", n)
	}
}

func TestInjectApprovalHookRefusesBrokenSettings(t *testing.T) {
	if _, err := injectApprovalHook([]byte("{not json"), "/bin/h"); err == nil {
		t.Fatal("settings we cannot parse must be an error, not a silently rewritten file")
	}
}

// `null` parses fine and yields a nil document. Assigning into it used to panic,
// which in a daemon with no recover() meant a hand-edited home could kill the
// process on `session create`.
func TestInjectApprovalHookTreatsNullSettingsAsEmpty(t *testing.T) {
	for _, in := range []string{"null", " null\n", "{}"} {
		out, err := injectApprovalHook([]byte(in), "/bin/h")
		if err != nil {
			t.Fatalf("injectApprovalHook(%q): %v", in, err)
		}
		hooks, ok := settingsMap(t, out)["hooks"].(map[string]any)
		if !ok {
			t.Fatalf("injectApprovalHook(%q) wrote no hooks:\n%s", in, out)
		}
		if n := len(hooks["PreToolUse"].([]any)); n != 1 {
			t.Fatalf("injectApprovalHook(%q): got %d PreToolUse entries, want 1", in, n)
		}
	}
}

// os.Executable can return a path with a space in it, and a host's binary is
// whatever the operator configured. The hook command goes through a shell, so an
// unquoted path would break every tool call in the session.
func TestInjectApprovalHookQuotesTheBinaryPath(t *testing.T) {
	for bin, want := range map[string]string{
		"/opt/my tools/herrscher": `'/opt/my tools/herrscher' approve hook`,
		"/opt/it's/herrscher":     `'/opt/it'\''s/herrscher' approve hook`,
	} {
		out, err := injectApprovalHook([]byte("{}"), bin)
		if err != nil {
			t.Fatalf("injectApprovalHook(%q): %v", bin, err)
		}
		hooks := settingsMap(t, out)["hooks"].(map[string]any)
		entry := hooks["PreToolUse"].([]any)[0].(map[string]any)
		got := entry["hooks"].([]any)[0].(map[string]any)["command"]
		if got != want {
			t.Fatalf("command = %q, want %q", got, want)
		}
	}
}

// writeHomeFiles lays down the three files MaterializeIntoAs reads.
func writeHomeFiles(t *testing.T, home string) {
	t.Helper()
	settings, err := buildSettings("")
	if err != nil {
		t.Fatalf("buildSettings: %v", err)
	}
	for name, data := range map[string][]byte{
		soulFile:     []byte("You are a test agent working in {{WORKTREE}}.\n"),
		mcpFile:      []byte(`{"mcpServers":{}}`),
		settingsFile: settings,
	} {
		if err := os.WriteFile(filepath.Join(home, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func TestMaterializeIntoAsCarriesTheFarSideBinary(t *testing.T) {
	home := t.TempDir()
	writeHomeFiles(t, home)
	dst := t.TempDir()
	a := Agent{Name: "a", Home: home}
	if err := a.MaterializeIntoAs(dst, "/far/worktree", "/far/bin/herrscher"); err != nil {
		t.Fatalf("MaterializeIntoAs: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dst, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(b), "'/far/bin/herrscher' approve hook") {
		t.Fatalf("the far side's binary is not in the settings:\n%s", b)
	}
	soul, err := os.ReadFile(filepath.Join(dst, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(soul), "/far/worktree") {
		t.Fatal("the worktree token must still be replaced with the far path")
	}
}

func TestMaterializeIntoAsWithoutBinaryWritesNoHook(t *testing.T) {
	home := t.TempDir()
	writeHomeFiles(t, home)
	dst := t.TempDir()
	a := Agent{Name: "a", Home: home}
	if err := a.MaterializeIntoAs(dst, "/far/worktree", ""); err != nil {
		t.Fatalf("MaterializeIntoAs: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dst, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, ok := settingsMap(t, b)["hooks"]; ok {
		t.Fatalf("a bypass session must carry no hook at all:\n%s", b)
	}
}
