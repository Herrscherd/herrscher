// Package agent models a durable companion agent: a persistent home directory
// holding the agent's persona (SOUL.md), its MCP server declaration (mcp.json),
// and its Claude settings (settings.json). The agent is materialized into a
// disposable session worktree by Agent.Materialize, which copies those files
// into the worktree as the files Claude Code auto-reads when its cwd is the
// worktree (.claude/CLAUDE.md, .mcp.json, .claude/settings.json). The model is
// domain-neutral: callers (e.g. Neublox's Roblox profile) supply the persona and
// MCP server; the package only stores and materializes them.
package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// File names inside an agent home (the durable source of truth).
const (
	soulFile     = "SOUL.md"
	mcpFile      = "mcp.json"
	settingsFile = "settings.json"
	tagsFile     = "TAGS"
	backendFile  = "backend"
	cmdFile      = "cmd"
)

// worktreeToken is replaced with the absolute worktree path when an agent is
// materialized, so an agent's mcp.json can point a server at the session's
// working directory without knowing it in advance.
const worktreeToken = "{{WORKTREE}}"

var materializedGitExcludes = []string{
	"/AGENTS.md",
	"/.codex/",
	"/.claude/",
	"/.mcp.json",
}

// Agent is a durable companion: a name, backend vendor, and the home directory
// that stores its persona and provisioning files.
type Agent struct {
	Name    string
	Home    string   // absolute path to the agent's home directory
	Tags    []string // capability tokens from <home>/TAGS (nil when absent), for host routing
	Backend string   // backend vendor from <home>/backend, empty when absent
	Cmd     string   // default invocation from <home>/cmd, empty when absent
}

// Materialize provisions the agent into a session worktree by writing the files
// Claude Code and Codex read from its working directory:
//
//	<worktree>/.mcp.json             (from <home>/mcp.json)
//	<worktree>/.claude/settings.json (from <home>/settings.json)
//	<worktree>/.claude/CLAUDE.md     (from <home>/SOUL.md — the layered persona)
//	<worktree>/AGENTS.md             (from <home>/SOUL.md)
//	<worktree>/.codex/config.toml    (converted from <home>/mcp.json)
//
// Any worktreeToken in a source file is replaced with the worktree path.
func (a Agent) Materialize(worktree string) error {
	if err := ensureLocalGitExcludes(worktree); err != nil {
		return err
	}
	claudeDir := filepath.Join(worktree, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}
	codexDir := filepath.Join(worktree, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		return fmt.Errorf("create .codex dir: %w", err)
	}
	copies := []struct{ src, dst string }{
		{filepath.Join(a.Home, mcpFile), filepath.Join(worktree, ".mcp.json")},
		{filepath.Join(a.Home, settingsFile), filepath.Join(claudeDir, "settings.json")},
		{filepath.Join(a.Home, soulFile), filepath.Join(claudeDir, "CLAUDE.md")},
		{filepath.Join(a.Home, soulFile), filepath.Join(worktree, "AGENTS.md")},
	}
	for _, c := range copies {
		buf, err := os.ReadFile(c.src)
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(c.src), err)
		}
		out := strings.ReplaceAll(string(buf), worktreeToken, worktree)
		if err := os.WriteFile(c.dst, []byte(out), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", c.dst, err)
		}
	}
	mcp, err := os.ReadFile(filepath.Join(a.Home, mcpFile))
	if err != nil {
		return fmt.Errorf("read %s: %w", mcpFile, err)
	}
	codexConfig, err := renderCodexMCP(mcp, worktree)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), codexConfig, 0o644); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	return nil
}

// ensureLocalGitExcludes keeps Herrscher-owned materialization out of Git's
// clean-worktree verdict without changing the project's committed .gitignore.
// Non-Git directories are valid shared-session targets and need no exclusion.
func ensureLocalGitExcludes(worktree string) error {
	inside, err := exec.Command("git", "-C", worktree, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil || strings.TrimSpace(string(inside)) != "true" {
		return nil
	}
	out, err := exec.Command("git", "-C", worktree, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return fmt.Errorf("resolve local git exclude: %w", err)
	}
	excludePath := strings.TrimSpace(string(out))
	if !filepath.IsAbs(excludePath) {
		excludePath = filepath.Join(worktree, excludePath)
	}
	current, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read local git exclude: %w", err)
	}
	existing := "\n" + strings.ReplaceAll(string(current), "\r\n", "\n") + "\n"
	var addition strings.Builder
	for _, pattern := range materializedGitExcludes {
		if !strings.Contains(existing, "\n"+pattern+"\n") {
			addition.WriteString(pattern)
			addition.WriteByte('\n')
		}
	}
	if addition.Len() == 0 {
		return nil
	}
	if len(current) > 0 && current[len(current)-1] != '\n' {
		additionText := "\n" + addition.String()
		addition.Reset()
		addition.WriteString(additionText)
	}
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open local git exclude: %w", err)
	}
	if _, err := f.WriteString(addition.String()); err != nil {
		_ = f.Close()
		return fmt.Errorf("write local git exclude: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close local git exclude: %w", err)
	}
	return nil
}
