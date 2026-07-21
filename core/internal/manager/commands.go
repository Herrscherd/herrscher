package manager

import contracts "github.com/Herrscherd/herrscher-contracts"

// Commands returns the manager's command set as neutral contracts.Cmd values for
// the CLI registry to dispatch. Each Run closes over the Handler's dependencies,
// so the registry holding these stays agnostic of the gateway, git, or the backend.
func (h *Handler) Commands() []contracts.Cmd {
	return []contracts.Cmd{
		contracts.New("session", "create").
			Help("create a session: a bridged channel + isolated worktree + backend").
			Param("name", "session name (slugified to a safe slug)", true).
			Param("project", "workspace sub-dir the backend works on", false).
			Param("clone", "remote repo (owner/name) to clone into the workspace first", false).
			Param("cmd", "bridged command (defaults to the configured cmd)", false).
			Param("backend", "bridge backend: stream (default) | oneshot", false).
			Param("vendor", "agent backend vendor: claude | codex | cursor", false).
			Param("gateways", "comma-separated gateway kinds to bind (e.g. chat,terminal)", false).
			Param("terminal_only", "bind the session to the terminal gateway only", false).
			Param("shared", "run in the main checkout instead of an isolated worktree", false).
			Param("agent", "provision the session from a durable agent (its persona + MCP + zero-prompt settings)", false).
			Param("extractor", "name a registered curation extractor to enable the P1 learning loop (empty = no learning)", false).
			Param("journal", "call-journal path Consolidate reads (worktree-relative ok); only used with extractor", false).
			Param("consolidate_every", "run Consolidate every N turns (0 = manual only); only used with extractor", false).
			Param("base", "existing ref the new worktree branches off (e.g. session/<A>); empty = fresh branch", false).
			Param("parent", "lead session that delegated this one (result-back P3); empty = none", false).
			Do(h.sessionCreateRun),
		contracts.New("session", "close").
			Help("close a session: stop the bridge, remove the worktree, archive the channel").
			Param("name", "session name", true).
			Param("force", "discard uncommitted worktree changes", false).
			Do(h.sessionCloseRun),
		contracts.New("session", "archive").
			Help("archive a session: stop the bridge, keep it resumable (row + transcript + resume token kept)").
			Param("name", "session name", true).
			Do(h.sessionArchiveRun),
		contracts.New("session", "list").
			Help("list active sessions").
			Do(h.sessionListRun),
		contracts.New("session", "who").
			Help("list the participants observed in a session").
			Param("name", "session name", true).
			Do(h.sessionWhoRun),
		contracts.New("session", "log").
			Help("dump a session's recorded transcript (scrollback); --json for the app").
			Param("name", "session name", true).
			Param("limit", "max entries to return (default 200, newest kept)", false).
			Do(h.sessionLogRun),
		contracts.New("session", "resume").
			Help("resume an archived session: clear archived + restart the bridge (revives with its stored transcript + resume token)").
			Param("name", "session name", true).
			Do(h.sessionResumeRun),
		contracts.New("agent", "create").
			Help("create a durable companion agent (persona + MCP + zero-prompt settings)").
			Param("name", "agent name (slugified to a safe slug)", true).
			Param("soul", "persona text written to SOUL.md (layered as .claude/CLAUDE.md)", false).
			Param("mcp", "stdio MCP server command line, e.g. 'neublox serve --project {{WORKTREE}}'", false).
			Param("backend", "agent backend vendor: claude | codex | cursor", false).
			Param("cmd", "default invocation carrying the model, e.g. 'codex --model gpt-5.6'", false).
			Param("tags", "space/comma-separated capability tokens (e.g. role:reviewer luau)", false).
			Do(h.agentCreateRun),
		contracts.New("agent", "list").
			Help("list durable companion agents (--json for the catalog)").
			Do(h.agentListRun),
		contracts.New("agent", "show").
			Help("show one agent's catalog record (name, tags, backend, soul); --json for Neublox").
			Param("name", "agent name", true).
			Do(h.agentShowRun),
		contracts.New("agent", "set-soul").
			Help("rewrite an agent's SOUL.md (applies to new sessions)").
			Param("name", "agent name", true).
			Param("soul", "new soul body", true).
			Do(h.agentSetSoulRun),
		contracts.New("set", "home").
			Help("set the category/forum that holds session channels").
			Param("channel", "category or forum channel id", true).
			Do(h.setHomeRun),
		contracts.New("set", "source").
			Help("set the source checkout `service update` builds from").
			Param("path", "absolute path to the source checkout", true).
			Do(h.setSourceRun),
		contracts.New("service", "restart").
			Help("restart the daemon").
			Do(h.serviceRestartRun),
		contracts.New("service", "update").
			Help("rebuild the daemon from source and restart it").
			Param("no_pull", "skip the git pull before building", false).
			Do(h.serviceUpdateRun),
	}
}
