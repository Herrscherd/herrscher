// Package agent models a durable companion agent: a persistent home directory
// holding the agent's persona (SOUL.md), its MCP server declaration (mcp.json),
// and its Claude settings (settings.json). The agent is materialized into a
// disposable session worktree by Agent.Materialize, which copies those files
// into the worktree as the files Claude Code auto-reads when its cwd is the
// worktree (.claude/CLAUDE.md, .mcp.json, .claude/settings.json). The model is
// domain-neutral: callers (e.g. Neublox's Roblox profile) supply the persona and
// MCP server; the package only stores and materializes them.
package agent

// File names inside an agent home (the durable source of truth).
const (
	soulFile     = "SOUL.md"
	mcpFile      = "mcp.json"
	settingsFile = "settings.json"
)

// worktreeToken is replaced with the absolute worktree path when an agent is
// materialized, so an agent's mcp.json can point a server at the session's
// working directory without knowing it in advance.
const worktreeToken = "{{WORKTREE}}"

// Agent is a durable companion: a name and the home directory that stores its
// persona and provisioning files.
type Agent struct {
	Name string
	Home string // absolute path to the agent's home directory
}
