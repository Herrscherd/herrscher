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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// File names inside an agent home (the durable source of truth).
const (
	soulFile     = "SOUL.md"
	userFile     = "USER.md"
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

// jsonStringInner returns s escaped for embedding inside a JSON string literal,
// without the surrounding quotes — so a worktree path with backslashes (Windows)
// or a quote can be spliced into a JSON template's string value and stay valid.
func jsonStringInner(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return s
	}
	return string(b[1 : len(b)-1])
}

var materializedGitExcludes = []string{
	"/AGENTS.md",
	"/.codex/",
	"/.claude/",
	"/.mcp.json",
	// The root the learned-skill projection owns. It holds generated SKILL.md
	// files, so without the exclude a session that learns anything would report a
	// dirty worktree, and every close would offer to discard work nobody wrote.
	"/.herrscher/",
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
//	<worktree>/.claude/USER.md       (from <home>/USER.md, when present)
//	<worktree>/.codex/config.toml    (converted from <home>/mcp.json)
//
// When <home>/USER.md exists, it is also referenced from CLAUDE.md via a
// Claude Code @import and inlined into AGENTS.md (Codex has no import
// mechanism). Without a USER.md, CLAUDE.md and AGENTS.md are byte-identical
// to SOUL.md.
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
	// jsonDst marks a destination whose worktree token lives inside a JSON
	// string: the path must be JSON-escaped, or a Windows worktree (backslashes)
	// or one with a quote would produce invalid JSON. The Codex TOML path is
	// escaped separately by renderCodexMCP. CLAUDE.md/AGENTS.md (markdown, raw
	// path) are composed below so USER.md can augment them.
	copies := []struct {
		src, dst string
		jsonDst  bool
	}{
		{filepath.Join(a.Home, mcpFile), filepath.Join(worktree, ".mcp.json"), true},
		{filepath.Join(a.Home, settingsFile), filepath.Join(claudeDir, "settings.json"), true},
	}
	for _, c := range copies {
		buf, err := os.ReadFile(c.src)
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(c.src), err)
		}
		repl := worktree
		if c.jsonDst {
			repl = jsonStringInner(worktree)
		}
		out := strings.ReplaceAll(string(buf), worktreeToken, repl)
		if err := os.WriteFile(c.dst, []byte(out), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", c.dst, err)
		}
	}

	// Persona (SOUL.md) → CLAUDE.md + AGENTS.md, optionally augmented with the
	// user profile (USER.md). Absent USER.md leaves both byte-identical to soul.
	soul, err := os.ReadFile(filepath.Join(a.Home, soulFile))
	if err != nil {
		return fmt.Errorf("read %s: %w", soulFile, err)
	}
	soulOut := strings.ReplaceAll(string(soul), worktreeToken, worktree)
	claudeMd, agentsMd := soulOut, soulOut

	userRaw, uErr := os.ReadFile(filepath.Join(a.Home, userFile))
	if uErr != nil && !os.IsNotExist(uErr) {
		return fmt.Errorf("read %s: %w", userFile, uErr)
	}
	if uErr == nil {
		userOut := strings.ReplaceAll(string(userRaw), worktreeToken, worktree)
		if err := os.WriteFile(filepath.Join(claudeDir, "USER.md"), []byte(userOut), 0o644); err != nil {
			return fmt.Errorf("write .claude/USER.md: %w", err)
		}
		// @USER.md, not @.claude/USER.md: Claude Code resolves relative imports
		// against the importing file's own directory, and CLAUDE.md already lives
		// in .claude/, so the profile is its sibling.
		claudeMd = soulOut + "\n\n@USER.md\n"
		agentsMd = soulOut + "\n\n# User\n\n" + userOut
	}

	if err := os.WriteFile(filepath.Join(claudeDir, "CLAUDE.md"), []byte(claudeMd), 0o644); err != nil {
		return fmt.Errorf("write .claude/CLAUDE.md: %w", err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "AGENTS.md"), []byte(agentsMd), 0o644); err != nil {
		return fmt.Errorf("write AGENTS.md: %w", err)
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
