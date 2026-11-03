package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mapOf(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("got %T, want a JSON object", v)
	}
	return m
}

func listOf(t *testing.T, v any) []any {
	t.Helper()
	l, ok := v.([]any)
	if !ok {
		t.Fatalf("got %T, want a JSON array", v)
	}
	return l
}

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
	perms := mapOf(t, m["permissions"])
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
	hooks := mapOf(t, settingsMap(t, out)["hooks"])
	if n := len(listOf(t, hooks["PreToolUse"])); n != 2 {
		t.Fatalf("got %d PreToolUse entries, want 2", n)
	}
}

func TestInjectApprovalHookRefusesBrokenSettings(t *testing.T) {
	if _, err := injectApprovalHook([]byte("{not json"), "/bin/h"); err == nil {
		t.Fatal("settings we cannot parse must be an error, not a silently rewritten file")
	}
}

func TestInjectApprovalHookRefusesHooksOfTheWrongType(t *testing.T) {
	for _, in := range []string{
		`{"hooks":"none"}`,
		`{"hooks":["PreToolUse"]}`,
		`{"hooks":{"PreToolUse":{"matcher":"*"}}}`,
		`{"hooks":{"PreToolUse":"mine"}}`,
	} {
		if _, err := injectApprovalHook([]byte(in), "/bin/h"); err == nil {
			t.Fatalf("injectApprovalHook(%s) rewrote settings it did not understand", in)
		}
	}
	for _, in := range []string{`{"hooks":null}`, `{"hooks":{"PreToolUse":null}}`} {
		out, err := injectApprovalHook([]byte(in), "/bin/h")
		if err != nil {
			t.Fatalf("injectApprovalHook(%s): %v", in, err)
		}
		hooks, ok := settingsMap(t, out)["hooks"].(map[string]any)
		if !ok {
			t.Fatalf("injectApprovalHook(%s) wrote no hooks:\n%s", in, out)
		}
		if n := len(listOf(t, hooks["PreToolUse"])); n != 1 {
			t.Fatalf("injectApprovalHook(%s): got %d PreToolUse entries, want 1", in, n)
		}
	}
}

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
		if n := len(listOf(t, hooks["PreToolUse"])); n != 1 {
			t.Fatalf("injectApprovalHook(%q): got %d PreToolUse entries, want 1", in, n)
		}
	}
}

func TestInjectApprovalHookQuotesTheBinaryPath(t *testing.T) {
	for bin, want := range map[string]string{
		"/opt/my tools/herrscher": `'/opt/my tools/herrscher' approve hook`,
		"/opt/it's/herrscher":     `'/opt/it'\''s/herrscher' approve hook`,
	} {
		out, err := injectApprovalHook([]byte("{}"), bin)
		if err != nil {
			t.Fatalf("injectApprovalHook(%q): %v", bin, err)
		}
		hooks := mapOf(t, settingsMap(t, out)["hooks"])
		entry := mapOf(t, listOf(t, hooks["PreToolUse"])[0])
		got := mapOf(t, listOf(t, entry["hooks"])[0])["command"]
		if got != want {
			t.Fatalf("command = %q, want %q", got, want)
		}
	}
}

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

func TestMaterializeHookWritesLocalSettings(t *testing.T) {
	dir := t.TempDir()
	if err := MaterializeHook(t.Context(), dir, "/usr/local/bin/herrscher"); err != nil {
		t.Fatalf("MaterializeHook: %v", err)
	}
	buf, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings.local.json: %v", err)
	}
	if !strings.Contains(string(buf), "approve hook") {
		t.Fatalf("no hook in %s", buf)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("settings.json was touched, err = %v", err)
	}
}

func TestMaterializeHookPreservesExistingLocalSettings(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claude, "settings.local.json"), []byte(`{"model":"opus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MaterializeHook(t.Context(), dir, "/bin/herrscher"); err != nil {
		t.Fatalf("MaterializeHook: %v", err)
	}
	buf, err := os.ReadFile(filepath.Join(claude, "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc := settingsMap(t, buf)
	if doc["model"] != "opus" {
		t.Fatalf("model key lost: %s", buf)
	}
	if doc["hooks"] == nil {
		t.Fatalf("no hooks key: %s", buf)
	}
}

func TestMaterializeHookNoBinIsNoOp(t *testing.T) {
	dir := t.TempDir()
	if err := MaterializeHook(t.Context(), dir, ""); err != nil {
		t.Fatalf("MaterializeHook: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
		t.Fatalf(".claude created, err = %v", err)
	}
}

func TestMaterializeHookTwiceLeavesOneHook(t *testing.T) {
	dir := t.TempDir()
	if err := MaterializeHook(t.Context(), dir, "/old/herrscher"); err != nil {
		t.Fatalf("first MaterializeHook: %v", err)
	}
	if err := MaterializeHook(t.Context(), dir, "/new/herrscher"); err != nil {
		t.Fatalf("second MaterializeHook: %v", err)
	}
	buf, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(buf), "approve hook"); n != 1 {
		t.Fatalf("%d hooks, want 1: %s", n, buf)
	}
	if strings.Contains(string(buf), "/old/herrscher") {
		t.Fatalf("stale binary path kept: %s", buf)
	}
}

func TestInjectApprovalHookKeepsForeignPreToolUseHooks(t *testing.T) {
	prior := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/usr/bin/lint"}]}]}}`)
	out, err := injectApprovalHook(prior, "/bin/herrscher")
	if err != nil {
		t.Fatalf("injectApprovalHook: %v", err)
	}
	if !strings.Contains(string(out), "/usr/bin/lint") {
		t.Fatalf("foreign hook dropped: %s", out)
	}
	if n := strings.Count(string(out), "approve hook"); n != 1 {
		t.Fatalf("%d approval hooks, want 1: %s", n, out)
	}
}
