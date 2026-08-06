# Herrscher

**Herrscher is a self-hosted harness for running fleets of AI coding agents — durable sessions with persistent memory, each in its own git worktree, reachable from Discord or your terminal.**

Agents survive restarts, learn across sessions, and delegate work to each other across model vendors.

Docs live in [Herrscherd/herrscher-docs](https://github.com/Herrscherd/herrscher-docs); page slugs below are given as inline code.

## What it gives you

- **Durable sessions** — each session is a channel + an agent + an isolated git worktree, supervised and restarted automatically; the conversation resumes from the backend's own resume token. A session can be created explicitly, or adopt an existing conversation — the Discord edge opens one the first time its owner pings the bot in a channel. → `architecture/durable-agents`, `architecture/session-lifecycle`
- **Persistent memory** — an auto-provisioned Obsidian vault, scoped shared-per-project and private-per-agent, recalled before every turn. → `architecture/memory`
- **Learning** — opt-in consolidation of a session's work into scoped nodes, with aging, semantic merge, cross-agent promotion, and fully reversible archiving. → `architecture/learning`
- **Multi-agent delegation across vendors** — an agent ends a turn with a `⟢` trailer to delegate, fan out, route, merge, or hand off; workers inherit their agent's vendor, so one run can mix Claude, Codex, and Cursor. → `architecture/coordination`
- **Any front end** — Discord, the in-tree terminal TUI, or your own gateway plugin behind the same neutral port. The TUI renders several sessions as live tabs, frames each request, answer and error between titled rules so a turn keeps its shape when colour does not survive, types each event by role (reasoning, tool family, notice, error), renders the agent's markdown and diffs, and carries a status bar with the session's cumulative cost and a gauge of how full its context window is. → `plugins/gateway`, `plugins/terminal`
- **Model routing** — each backend declares the models it offers in its manifest, so the catalog is queryable without instantiating anything. A route policy picks which of them a build may use: `all` runs on this machine's own vendor logins, `gateway-only` runs every turn through a gateway you supply and refuses the two ways around the catalog (a free-form `--cmd`, or a session with no model at all).

Also: cross-backend skills (`SKILL.md` works on every backend), including the playbooks the binary ships with and installs into `~/.claude/skills` on first run — existing files are never overwritten, so yours stay yours → `guide/skills`; and per-node memory budgets → `guide/budgets`.

## Install & run

Requires Go 1.25+.

```bash
git clone https://github.com/Herrscherd/herrscher.git
cd herrscher
go build -o herrscher .    # the only binary; plugins are compiled in
./herrscher init           # compose the plugin stack, seed config, choose routing
./herrscher                # serves, with the terminal TUI in the foreground
```

One daemon per state file: a second one over the same state answers every message twice and bills it twice. So a bare `herrscher` on a TTY **serves when nothing else is**, and **attaches** when the service already is — the same TUI, driven over the sockets the running daemon exposes, so you see and drive the agent that is actually live. Quitting the window leaves that daemon running. Off a TTY there is nothing to attach and the second daemon is refused, naming the process that holds the state; use `--state` if you really want a separate world.

`init` ends by asking where agent turns run: the route policy, the gateway credentials `gateway-only` requires (both halves or neither — a base URL without a token would redirect the CLI while it keeps authenticating as this machine's own account), and the model a session gets when it names none. It writes them to `.env`; `herrscher models list` shows what the active policy offers.

**No Discord token is needed for the terminal path** — the in-tree terminal gateway is a first-class gateway, so you can create, drive, and resume sessions entirely from your shell. A Discord token is only required if you enable the Discord gateway. For a boot-started service, an Arch `PKGBUILD`, and the rest, see `guide/installation` and `guide/service`.

## Plugins

Plugins compile **into** the binary (the xcaddy pattern): add a blank import, rebuild. `herrscher update` bumps every compiled-in plugin, rebuilds, and reinstalls the binary — restart the service afterwards to run it.

| Category | Port | Official plugin |
|----------|------|-----------------|
| Gateway | `Gateway` | [herrscher-discord-gateway], in-tree `terminal` |
| Backend | `Backend` | [herrscher-claude-backend], [herrscher-codex-backend], [herrscher-cursor-backend] |
| Memory | `Memory` | [herrscher-obsidian-memory] |
| Orchestrator | `Orchestrator` | [herrscher-orchestrator] |

[herrscher-discord-gateway]: https://github.com/Herrscherd/herrscher-discord-gateway
[herrscher-claude-backend]: https://github.com/Herrscherd/herrscher-claude-backend
[herrscher-codex-backend]: https://github.com/Herrscherd/herrscher-codex-backend
[herrscher-cursor-backend]: https://github.com/Herrscherd/herrscher-cursor-backend
[herrscher-obsidian-memory]: https://github.com/Herrscherd/herrscher-obsidian-memory
[herrscher-orchestrator]: https://github.com/Herrscherd/herrscher-orchestrator

## Architecture

Hexagonal. [herrscher-contracts](https://github.com/Herrscherd/herrscher-contracts) is the authority: it declares the ports and neutral types, with zero platform mechanics. Every dependency arrow points inward at it — the agnostic domain (`core`) depends on no edge, the edges depend on no core, and only the host's wiring file ever sees two concrete types at once. Purity tests (`TestHostPurity`, `TestCorePurity`, `TestCoreNamesNoConcretePlatform`) fail the build if a concrete platform leaks in. → `architecture/hexagonal`

## Documentation

Full docs: [Herrscherd/herrscher-docs](https://github.com/Herrscherd/herrscher-docs) — `overview/introduction`, `architecture/hexagonal`, `plugins/model`, `guide/quickstart`, `reference/env`.

## A note on history

The platform grew out of a Go monolith that bridged Discord to a local
Claude. Herrscher is that monolith decomposed along its natural seams — channel,
model, domain — so each can evolve independently. The contract shapes were chosen
deliberately to make the eventual transport change (in-process → NATS/gRPC) a
detail, not a rewrite.
