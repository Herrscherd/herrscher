package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeWritesFilesWithSubstitution(t *testing.T) {
	s := NewStore(t.TempDir())
	a, err := s.Create(CreateSpec{Name: "roblox", Soul: "PERSONA", MCP: "neublox serve --project {{WORKTREE}}"})
	if err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	if err := a.Materialize(wt); err != nil {
		t.Fatal(err)
	}

	mcp, _ := os.ReadFile(filepath.Join(wt, ".mcp.json"))
	if strings.Contains(string(mcp), "{{WORKTREE}}") {
		t.Fatalf("token not substituted:\n%s", mcp)
	}
	if !strings.Contains(string(mcp), wt) {
		t.Fatalf("worktree path not injected:\n%s", mcp)
	}
	if !strings.Contains(string(mcp), `"neublox"`) {
		t.Fatalf("neublox server missing:\n%s", mcp)
	}

	settings, _ := os.ReadFile(filepath.Join(wt, ".claude", "settings.json"))
	if !strings.Contains(string(settings), "enableAllProjectMcpServers") {
		t.Fatalf("settings missing:\n%s", settings)
	}

	claude, _ := os.ReadFile(filepath.Join(wt, ".claude", "CLAUDE.md"))
	if string(claude) != "PERSONA" {
		t.Fatalf("CLAUDE.md = %q", claude)
	}

	agents, err := os.ReadFile(filepath.Join(wt, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if string(agents) != "PERSONA" {
		t.Fatalf("AGENTS.md = %q", agents)
	}

	codex, err := os.ReadFile(filepath.Join(wt, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("read .codex/config.toml: %v", err)
	}
	escapedWorktree := strings.ReplaceAll(wt, `\`, `\\`)
	if !strings.Contains(string(codex), "[mcp_servers.neublox]") ||
		!strings.Contains(string(codex), `command = "neublox"`) ||
		!strings.Contains(string(codex), escapedWorktree) {
		t.Fatalf("unexpected Codex config:\n%s", codex)
	}
}

func TestMaterializeMissingHomeErrors(t *testing.T) {
	a := Agent{Name: "ghost", Home: filepath.Join(t.TempDir(), "ghost")}
	if err := a.Materialize(t.TempDir()); err == nil {
		t.Fatal("expected error materializing an agent with no home files")
	}
}

func TestMaterializeKeepsFreshSessionWorktreeClean(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	worktree := filepath.Join(t.TempDir(), "session")
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "-b", "session/test", worktree).CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}

	store := NewStore(t.TempDir())
	a, err := store.Create(CreateSpec{Name: "roblox", Soul: "PERSONA", MCP: "neublox serve"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Materialize(worktree); err != nil {
		t.Fatal(err)
	}

	status, err := exec.Command("git", "-C", worktree, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, status)
	}
	if len(status) != 0 {
		t.Fatalf("fresh materialized worktree is dirty:\n%s", status)
	}
	for _, path := range []string{".claude", ".codex", ".mcp.json", "AGENTS.md"} {
		if out, err := exec.Command("git", "-C", worktree, "check-ignore", "--quiet", "--", path).CombinedOutput(); err != nil {
			t.Fatalf("%s is not locally ignored: %v\n%s", path, err, out)
		}
	}
	if _, err := os.Stat(filepath.Join(worktree, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("materialization must not create or modify project .gitignore: %v", err)
	}

	if err := os.WriteFile(filepath.Join(worktree, "user.txt"), []byte("user work"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err = exec.Command("git", "-C", worktree, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git status after user file: %v\n%s", err, status)
	}
	if !strings.Contains(string(status), "user.txt") {
		t.Fatalf("non-owned user file must remain dirty:\n%s", status)
	}
}

func TestMaterializeWithUserProfile(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "SOUL.md"), []byte("# Soul"), 0o644)
	os.WriteFile(filepath.Join(home, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o644)
	os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{}`), 0o644)
	os.WriteFile(filepath.Join(home, "USER.md"), []byte("user works at {{WORKTREE}}"), 0o644)

	wt := t.TempDir()
	a := Agent{Name: "alice", Home: home}
	if err := a.Materialize(wt); err != nil {
		t.Fatal(err)
	}

	user, err := os.ReadFile(filepath.Join(wt, ".claude", "USER.md"))
	if err != nil {
		t.Fatalf("read .claude/USER.md: %v", err)
	}
	if string(user) != "user works at "+wt {
		t.Fatalf("worktree token not substituted: %q", user)
	}
	claude, _ := os.ReadFile(filepath.Join(wt, ".claude", "CLAUDE.md"))
	// The import must be @USER.md (sibling), not @.claude/USER.md: Claude Code
	// resolves relative imports against CLAUDE.md's own .claude/ directory, so a
	// .claude/ prefix would point at the nonexistent .claude/.claude/USER.md.
	if !strings.Contains(string(claude), "# Soul") || !strings.Contains(string(claude), "@USER.md") {
		t.Fatalf("CLAUDE.md missing soul or import: %q", claude)
	}
	if strings.Contains(string(claude), "@.claude/USER.md") {
		t.Fatalf("CLAUDE.md uses wrong import path @.claude/USER.md: %q", claude)
	}
	agents, _ := os.ReadFile(filepath.Join(wt, "AGENTS.md"))
	if !strings.Contains(string(agents), "# User") || !strings.Contains(string(agents), "user works at "+wt) {
		t.Fatalf("AGENTS.md missing inlined user: %q", agents)
	}
}

func TestMaterializeWithoutUserProfileUnchanged(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "SOUL.md"), []byte("# Soul"), 0o644)
	os.WriteFile(filepath.Join(home, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o644)
	os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{}`), 0o644)

	wt := t.TempDir()
	a := Agent{Name: "alice", Home: home}
	if err := a.Materialize(wt); err != nil {
		t.Fatal(err)
	}
	claude, _ := os.ReadFile(filepath.Join(wt, ".claude", "CLAUDE.md"))
	agents, _ := os.ReadFile(filepath.Join(wt, "AGENTS.md"))
	if string(claude) != "# Soul" || string(agents) != "# Soul" {
		t.Fatalf("no-profile output must equal soul verbatim: claude=%q agents=%q", claude, agents)
	}
	if _, err := os.Stat(filepath.Join(wt, ".claude", "USER.md")); !os.IsNotExist(err) {
		t.Fatal(".claude/USER.md must not exist without a home USER.md")
	}
}
