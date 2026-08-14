# Herrscher

**Herrscher is a self-hosted harness for running fleets of AI coding agents — durable sessions with persistent memory, each in its own git worktree, reachable from Discord or your terminal.**

Agents survive restarts, learn across sessions, and delegate work to each other across model vendors.

Docs live in [Herrscherd/herrscher-docs](https://github.com/Herrscherd/herrscher-docs); page slugs below are given as inline code.

## What it gives you

- **Durable sessions** — each session is a channel + an agent + an isolated git worktree, supervised and restarted automatically; the conversation resumes from the backend's own resume token. A session can be created explicitly, or adopt an existing conversation (`--channel_id`) — the Discord edge opens one the first time its owner pings the bot in a channel. A new one lands in the daemon's home unless `--under` names another category or forum, so work opened from one server does not surface in another. → `architecture/durable-agents`, `architecture/session-lifecycle`
- **Persistent memory** — an auto-provisioned Obsidian vault, scoped shared-per-project and private-per-agent, recalled before every turn. → `architecture/memory`
- **Learning** — opt-in consolidation of a session's work into scoped nodes, with aging, semantic merge, cross-agent promotion, and fully reversible archiving. → `architecture/learning`
- **Multi-agent delegation across vendors** — an agent ends a turn with a `⟢` trailer to delegate, fan out, route, merge, or hand off; workers inherit their agent's vendor, so one run can mix Claude, Codex, and Cursor. → `architecture/coordination`
- **An agent that knows what it is running inside** — every turn's prompt carries a `<capabilities>` block: that this session is supervised by a running daemon, that the daemon is reachable from its own shell as `herrscher <verb>` with the credentials it already holds, and the list of verbs that daemon dispatches, one line per family. The list comes from the daemon's registry at spawn, over the environment beside the gateway credentials, because a gateway's contributed verbs exist only where that gateway is instantiated — so the block cannot advertise less than the daemon actually answers. Without it an agent asked to read a chat channel reaches for a token of its own and concludes the platform is unreachable, while the daemon behind it holds that gateway open.
- **Any front end** — Discord, the in-tree terminal TUI, or your own gateway plugin behind the same neutral port. The TUI renders several sessions as live tabs, opens each answer with a titled rule so a turn keeps its shape when colour does not survive — and rules nothing else, since a frame drawn around every turn marks none of them — types each event by role (reasoning, tool family, notice, error), renders the agent's markdown and diffs, replays a resumed session's history as the turns it was, and carries a status bar with the session's cumulative cost and a gauge of how full its context window is (spelled out on demand with `/usage`). Its `/` palette is derived from the daemon's own registry — `commands --json`, filtered to the verbs a tab may run — so the menu cannot fall behind what the daemon dispatches, and scrolls rather than truncating, so every verb is reachable with the arrows. An attached window follows the live turn because every driven session taps the daemon's event socket, and Esc stops a turn as an interruption rather than an error, keeping whatever the agent had already written. Picking up a chat session's tab and typing there answers you *there*: a turn says where it was said, so it is rendered in the window that asked and not posted a second time in the channel the conversation came from, where nobody asked anything. → `plugins/gateway`, `plugins/terminal`
- **A TUI that uses the terminal it is in** — capabilities are probed once at startup, and every feature that has a richer and a plainer form reads that one answer. Images render through the kitty graphics protocol where it exists, sixel where it does not, and Unicode half-blocks everywhere else, so a picture is shown on any terminal that can draw text; PNG, JPEG, GIF and WebP all decode, and an oversized one is downscaled rather than dropped. Links the transcript already contained become real OSC 8 hyperlinks where the terminal has them and styled text where it does not. Degradation is silent at the point of use: nothing announces that a protocol is missing, it simply draws the next best thing. `/capabilities` says what is live, what is not, and what each unavailable feature falls back to.
- **Nothing opens on its own** — a link is followed because the operator asked, never because a message arrived or a turn completed. `ctrl+l` walks the links in the transcript, and while one is selected the status line shows its *resolved target* — a markdown label can name one destination and point at another — and `ctrl+o` opens it: a URL through the desktop handler, a `file.go:42` reference through `$EDITOR` at that line. That is display, not a confirmation prompt; the keypress is the consent.
- **Reading a long session** — `ctrl+s` searches the transcript as you type, marks every match, cycles them with `ctrl+n`/`ctrl+p`, and puts the scroll position back where it was when you close it. `alt+↑`/`alt+↓` jump by turn, `ctrl+t` collapses the history above the current exchange to one line per turn, `ctrl+f` folds long code blocks to a summary line, and `ctrl+y` copies the last one to the clipboard as raw text. Tool lines carry a budget of two rows apiece — unbounded, a compound shell command becomes a paragraph in a single colour, and the session path it opens with is identical on every call — so a continuation hangs under the target it belongs to, an absolute path deep enough to cost a row is said in the two segments that identify it, and what is left over is counted rather than dropped: `+3 ↵`. `alt+e` lifts the budget and spells every command out as it was run. Diffs are coloured by hunk (and by the basic ANSI pair on a 16-colour terminal, where a hex green would be approximated away), and tables truncate their widest column instead of wrapping into confetti.
- **Getting text back out** — the window runs on the alternate screen with the mouse captured, which is what lets the wheel scroll a transcript the terminal keeps no scrollback for, and is also why click-drag selection is unavailable exactly where the interesting text is. So there are two ways out. `alt+y` puts the last answer on the clipboard, and `/copy [reply|turn|all]` takes the answer, the whole exchange that produced it, or the tab's transcript — what was said only, never this window's tool lines, reasoning, costs or notices, which are noise in a paste. And `ctrl+g` (or `/mouse`) hands the mouse back to the terminal so its own selection works as always; the status bar says `mouse → terminal` while it is released, `pgup`/`pgdn` still scroll, and `ctrl+g` takes it back.
- **Numbers that mean what they say** — the context gauge measures the live prompt, read from the counters that arrive mid-turn; the reply that ends a turn carries the vendor's *billing* totals for it, which is what the cost line and the budget need and what the window would have shown as an impossible `1252.2k/200k · 100%`.
- **Model routing** — each backend declares the models it offers in its manifest, so the catalog is queryable without instantiating anything. A route policy picks which of them a build may use: `all` runs on this machine's own vendor logins, `gateway-only` runs every turn through a gateway you supply and refuses the two ways around the catalog (a free-form `--cmd`, or a session with no model at all).

Also: cross-backend skills (`SKILL.md` works on every backend), including the playbooks the binary ships with and installs into `~/.claude/skills` — kept current, because a playbook is instructions and a stale one is the agent following last release's rules: a copy still bytewise the one we wrote is replaced when the shipped text changes, and a copy you edited is left alone and named at startup, so you learn the rewrite is not in effect instead of finding your version gone → `guide/skills`; and per-node memory budgets → `guide/budgets`.

## Install & run

Requires Go 1.25+.

```bash
git clone https://github.com/Herrscherd/herrscher.git
cd herrscher
go build -o herrscher .    # the only binary; plugins are compiled in
./herrscher init           # compose the plugin stack, seed config, choose routing
./herrscher                # serves, with the terminal TUI in the foreground
./herrscher "read the thread and propose a split"   # a session on that task, in the TUI
./herrscher --print "…"                             # one turn, the reply on stdout
```

An argument that carries whitespace is a task, not a verb: it opens a session of
its own — isolated worktree, terminal-only, named after the task's opening words
— sends the task there, and opens the TUI on that tab, so the turn runs in front
of you and you can answer it. Whether the window attaches to the running daemon
or starts one is the same question a bare `herrscher` asks (below); either way
the session is persistent, and closing the window does not end it: `herrscher
session list` shows it, `session seed --name` continues it, `session close` ends
it.

`--print` is the non-interactive form: one turn, the reply on stdout and the
session name on stderr, so `herrscher --print "…" > out.md` holds the answer
alone. A run with no terminal takes that form on its own, since there is nothing
there to draw a window on.

A single unrecognised word stays a verb, so a typo is still a typo; `-p` (or
`--print`) forces a one-word task.

A bare `herrscher` starts something rather than resuming it: every launch opens on a conversation of its own, named after the directory you launched in (`herrscher-2` if that name is taken, a random `s-xxxx` if the directory cannot be one). What came before is still there — a live tab if it is still running, `/resume` otherwise. Nothing piles up either: a session nobody spoke in is archived when the window closes. In the window, `/session close` closes the session you are looking at (`/session close --name <other>` closes another, `--force` discards uncommitted work), which is what Ctrl+W on an empty composer asks to confirm.

One daemon per state file: a second one over the same state answers every message twice and bills it twice. So a bare `herrscher` on a TTY **serves when nothing else is**, and **attaches** when the service already is — the same TUI, driven over the sockets the running daemon exposes, so you see and drive the agent that is actually live. Quitting the window leaves that daemon running. Off a TTY there is nothing to attach and the second daemon is refused, naming the process that holds the state; use `--state` if you really want a separate world.

`init` ends by asking where agent turns run: the route policy, the gateway credentials `gateway-only` requires (both halves or neither — a base URL without a token would redirect the CLI while it keeps authenticating as this machine's own account), and the model a session gets when it names none. It writes them to `.env`; `herrscher models list` shows what the active policy offers.

**No Discord token is needed for the terminal path** — the in-tree terminal gateway is a first-class gateway, so you can create, drive, and resume sessions entirely from your shell. A Discord token is only required if you enable the Discord gateway. Installed as a service, the daemon keeps the `PATH` of the shell that installed it — a launcher supplies almost nothing, and an agent cannot use a tool it cannot find — minus the entries that cannot outlive that shell, since a service resolving commands through a directory anyone can recreate would trust whoever got there first. For a boot-started service, an Arch `PKGBUILD`, and the rest, see `guide/installation` and `guide/service`.

## Plugins

Plugins compile **into** the binary (the xcaddy pattern): add a blank import, rebuild. `herrscher update` bumps every unpinned compiled-in plugin, rebuilds, and reinstalls the binary — restart the service afterwards to run it.

Each plugin carries its own version. `herrscher plugin list` reports, per module, the version installed, whether it is pinned, and the newest published — the last needs the network, and reads `?` without one. `herrscher plugin add <module>@<version>` installs a chosen version; `herrscher plugin pin <module> [<version>]` moves it there first when a version is given, then records the module as pinned; `herrscher plugin unpin <module>` drops the record. `herrscher update` skips every pinned module and names each one it skipped, so a pin never looks like a silent no-op. The pins live in `.herrscher-pins` beside `plugins.go` — one module path per line, `#` comments ignored, no versions, since `go.mod` already holds those and a second copy could disagree.

Every change to the composition is a transaction with two questions in it. Before writing, the CLI reports what can be known against the change — that it moves a module backwards, or that it wants a different `herrscher-contracts` than the composition resolves — and asks to proceed. It then saves `go.mod`, `go.sum` and `plugins.go`, and runs `go get → go mod tidy → go build ./... → go install .`. When the build refuses, it prints the compiler's own error and offers two outcomes: restore the three files and leave the tree exactly as it was, or keep it as it is to repair by hand. A run with `--yes`, or with no terminal to ask, takes the safe branch at both — it proceeds, and it restores. The same flow is available from the TUI's `/plugins` screen, where both questions are select menus and a successful change ends on a line saying it applies at the next restart.

A plugin may also contribute commands of its own and the skills that teach an agent to use them. The daemon namespaces each contributed verb under the plugin's kind — the Discord gateway declares `channel read`, an operator types `discord channel read` — so the prefix is imposed by the host and two plugins can never collide; and its skills install only when that plugin is in the build, so a Discord playbook never sits in the context of a machine that has no Discord. Skills are a category of their own too: a plugin may contribute nothing but playbooks, and it is added, listed, pinned and removed by the same commands as any other. What it hands over is decided at startup rather than at compile time — a factory that looks at the machine and finds the tool its playbook describes is not installed returns nothing, and the daemon says which plugin declined and why.

| Category | Port | Official plugin |
|----------|------|-----------------|
| Gateway | `Gateway` | [herrscher-discord-gateway], in-tree `terminal` |
| Backend | `Backend` | [herrscher-claude-backend], [herrscher-codex-backend], [herrscher-cursor-backend] |
| Memory | `Memory` | [herrscher-obsidian-memory] |
| Orchestrator | `Orchestrator` | [herrscher-orchestrator] |
| Skills | — (playbooks only) | [herrscher-superset-skills] |

[herrscher-discord-gateway]: https://github.com/Herrscherd/herrscher-discord-gateway
[herrscher-claude-backend]: https://github.com/Herrscherd/herrscher-claude-backend
[herrscher-codex-backend]: https://github.com/Herrscherd/herrscher-codex-backend
[herrscher-cursor-backend]: https://github.com/Herrscherd/herrscher-cursor-backend
[herrscher-obsidian-memory]: https://github.com/Herrscherd/herrscher-obsidian-memory
[herrscher-orchestrator]: https://github.com/Herrscherd/herrscher-orchestrator
[herrscher-superset-skills]: https://github.com/Herrscherd/herrscher-superset-skills

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
