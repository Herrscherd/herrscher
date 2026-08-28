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
	"time"
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
	hostFile     = "host"
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

// hookWaitSeconds bounds how long the vendor waits for our hook. It is longer
// than the daemon's own wait on a human on purpose: if the vendor gave up
// first, the model would be told the hook crashed instead of being told why its
// tool call was refused.
const hookWaitSeconds = 900

// HookWait is hookWaitSeconds as a duration, exported for the one caller that
// has to compare a configured wait against it: a wait longer than this outlives
// the hook itself, and what the vendor does when the hook is gone is run the
// tool call.
const HookWait = hookWaitSeconds * time.Second

// shellSingleQuote quotes s for a POSIX shell, which is what Claude Code runs a
// hook command through. Without it a binary path holding a space (os.Executable
// can return one, and a host's configured binary is whatever the operator
// typed) would be read as a command plus an argument, and every tool call in
// the session would fail. An embedded single quote closes the quoting, is
// backslash-escaped outside it, then reopens it: the classic POSIX dance.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// injectApprovalHook adds the PreToolUse hook that asks the daemon before each
// tool call.
//
// It is added here rather than written into the agent's home because the binary
// it invokes belongs to the machine the session runs on, and a home does not
// know which machine that will be.
//
// Any hook herrscher wrote before is dropped first, so materializing twice over
// the same file leaves exactly one. A reused worktree already carries the entry
// from its first session, and a second one would ask the operator twice for
// every single tool call. Dropping also repairs the binary path when the daemon
// moved between the two materializations. Hooks somebody else wrote are left
// where they are: only a command that ends in this daemon's own verb matches.
func injectApprovalHook(settings []byte, herrscherBin string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(settings, &doc); err != nil {
		return nil, fmt.Errorf("settings.json: %w", err)
	}
	// A settings file whose whole content is the JSON literal `null` parses
	// without error and leaves doc nil. Treat it as the empty object it means,
	// rather than assigning into a nil map and taking the daemon down.
	if doc == nil {
		doc = map[string]any{}
	}
	// A key of the wrong type is refused rather than replaced. Both of these
	// hold operator configuration, and a settings file whose shape we do not
	// recognise is one we have no business rewriting: dropping the key would
	// throw away hooks somebody wrote, and say nothing. Materialization fails,
	// which names the file and stops the session before it starts.
	hooks := map[string]any{}
	if raw, ok := doc["hooks"]; ok && raw != nil {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("settings.json: hooks is %T, not an object", raw)
		}
		hooks = m
	}
	var pre []any
	if raw, ok := hooks["PreToolUse"]; ok && raw != nil {
		list, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("settings.json: hooks.PreToolUse is %T, not a list", raw)
		}
		pre = list
	}
	kept := make([]any, 0, len(pre)+1)
	for _, entry := range pre {
		if !isApprovalHook(entry) {
			kept = append(kept, entry)
		}
	}
	hooks["PreToolUse"] = append(kept, map[string]any{
		"matcher": "*",
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": shellSingleQuote(herrscherBin) + " approve hook",
			"timeout": hookWaitSeconds,
		}},
	})
	doc["hooks"] = hooks
	return json.MarshalIndent(doc, "", "  ")
}

// isApprovalHook reports whether one PreToolUse entry is a hook this daemon
// wrote. It matches on the verb the command ends with rather than on the whole
// command line, since the binary path in front of it is the one thing that
// legitimately differs between two materializations of the same worktree.
func isApprovalHook(entry any) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	list, ok := m["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range list {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if cmd, ok := hm["command"].(string); ok && strings.HasSuffix(cmd, " approve hook") {
			return true
		}
	}
	return false
}

// SelfBin is the herrscher binary running this process, which is what a hook
// materialized for THIS machine must invoke. Empty when it cannot be named, and
// an empty binary means no hook rather than a hook that fails on every call.
//
// That is a session materialized without the guardrail it asked for, so it is
// said out loud on the daemon's stderr, where the other approval warnings land.
// Warned and not repaired: there is nothing to fall back to, and refusing to
// create the session would trade a weaker session for no session at all.
func SelfBin() string {
	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "approvals: this binary cannot name itself, so sessions materialize with no approval hook: %v\n", err)
		return ""
	}
	return bin
}

// localSettingsFile is the settings file Claude Code layers over settings.json
// and writes itself for local permissions.
const localSettingsFile = "settings.local.json"

// MaterializeHook writes the PreToolUse approval hook into a worktree that has
// no agent to materialize. It is what puts an agent-less session's tool calls
// in front of the approval policy, which before this they never were.
//
// It targets settings.local.json rather than settings.json because this
// worktree is cut from the project tip: .claude/settings.json there may be a
// tracked file belonging to the repository, and rewriting it would dirty the
// working tree and risk being committed. settings.local.json is the layer
// Claude Code already treats as machine-local, and EnsureGitExcludes keeps it
// out of git status.
//
// An empty herrscherBin means no hook at all rather than a hook that fails on
// every tool call; SelfBin already says out loud when it cannot name itself.
func MaterializeHook(worktree, herrscherBin string) error {
	if herrscherBin == "" {
		return nil
	}
	if err := EnsureGitExcludes(worktree); err != nil {
		return err
	}
	claudeDir := filepath.Join(worktree, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}
	path := filepath.Join(claudeDir, localSettingsFile)
	// A missing file is the common case and means the empty object. Any other
	// read error is real: papering over it with an empty document would
	// silently discard whatever the operator had configured there.
	cur := []byte("{}")
	buf, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(strings.TrimSpace(string(buf))) > 0 {
			cur = buf
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("read %s: %w", path, err)
	}
	out, err := injectApprovalHook(cur, herrscherBin)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
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
	// Host is the default place this agent's sessions run, from <home>/host.
	// Empty = this machine. A session's explicit --host still wins: this is a
	// default, not a rule.
	Host string
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
// The settings file is the one that is not merely copied: a PreToolUse hook
// calling this binary's `approve hook` is injected into it, which is what puts
// the session's tool calls in front of the approval policy. It goes there and
// nowhere else, so it binds Claude Code and not the other vendors materialized
// beside it. MaterializeAs takes the binary to call and materializes no hook at
// all when given none.
//
// Any worktreeToken in a source file is replaced with the worktree path.
func (a Agent) Materialize(worktree string) error {
	return a.MaterializeAs(worktree, SelfBin())
}

// MaterializeAs is Materialize with the hook's binary, empty for no hook.
func (a Agent) MaterializeAs(worktree, herrscherBin string) error {
	if err := EnsureGitExcludes(worktree); err != nil {
		return err
	}
	return a.MaterializeIntoAs(worktree, worktree, herrscherBin)
}

// MaterializeInto materializes for this machine, hook included.
func (a Agent) MaterializeInto(dst, worktreePath string) error {
	return a.MaterializeIntoAs(dst, worktreePath, SelfBin())
}

// MaterializeIntoAs writes the agent's provisioning files into dst, substituting
// worktreePath wherever the worktree token appears. Materialize is the case
// where the two are the same directory; they differ when the files are staged
// here to be shipped to a worktree on another machine, where the path written
// into them must be the one over there.
//
// herrscherBin is the herrscher binary the materialized approval hook must
// invoke, empty for no hook at all. The binary and the worktree are separate
// parameters because the first belongs to the machine and the second to the
// session, and the staged case is exactly where the two machines differ.
//
// It does not touch git: the local excludes belong with the repository, which
// in the staged case is not on this machine.
func (a Agent) MaterializeIntoAs(dst, worktreePath, herrscherBin string) error {
	claudeDir := filepath.Join(dst, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}
	codexDir := filepath.Join(dst, ".codex")
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
		settings bool
	}{
		{filepath.Join(a.Home, mcpFile), filepath.Join(dst, ".mcp.json"), true, false},
		{filepath.Join(a.Home, settingsFile), filepath.Join(claudeDir, "settings.json"), true, true},
	}
	for _, c := range copies {
		buf, err := os.ReadFile(c.src)
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(c.src), err)
		}
		repl := worktreePath
		if c.jsonDst {
			repl = jsonStringInner(worktreePath)
		}
		out := []byte(strings.ReplaceAll(string(buf), worktreeToken, repl))
		if c.settings && herrscherBin != "" {
			if out, err = injectApprovalHook(out, herrscherBin); err != nil {
				return err
			}
		}
		if err := os.WriteFile(c.dst, out, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", c.dst, err)
		}
	}

	// Persona (SOUL.md) → CLAUDE.md + AGENTS.md, optionally augmented with the
	// user profile (USER.md). Absent USER.md leaves both byte-identical to soul.
	soul, err := os.ReadFile(filepath.Join(a.Home, soulFile))
	if err != nil {
		return fmt.Errorf("read %s: %w", soulFile, err)
	}
	soulOut := strings.ReplaceAll(string(soul), worktreeToken, worktreePath)
	claudeMd, agentsMd := soulOut, soulOut

	userRaw, uErr := os.ReadFile(filepath.Join(a.Home, userFile))
	if uErr != nil && !os.IsNotExist(uErr) {
		return fmt.Errorf("read %s: %w", userFile, uErr)
	}
	if uErr == nil {
		userOut := strings.ReplaceAll(string(userRaw), worktreeToken, worktreePath)
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
	if err := os.WriteFile(filepath.Join(dst, "AGENTS.md"), []byte(agentsMd), 0o644); err != nil {
		return fmt.Errorf("write AGENTS.md: %w", err)
	}

	mcp, err := os.ReadFile(filepath.Join(a.Home, mcpFile))
	if err != nil {
		return fmt.Errorf("read %s: %w", mcpFile, err)
	}
	codexConfig, err := renderCodexMCP(mcp, worktreePath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), codexConfig, 0o644); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	return nil
}

// EnsureGitExcludes keeps Herrscher-owned materialization out of Git's
// clean-worktree verdict without changing the project's committed .gitignore.
// Non-Git directories are valid shared-session targets and need no exclusion.
// Exported because on a remote host it runs where the repository is, from the
// worktree verb, rather than beside the file writes.
func EnsureGitExcludes(worktree string) error {
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
