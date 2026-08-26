# Herrscher

**A self-hosted harness for running fleets of AI coding agents.**

A session is a conversation, an agent, and an isolated git worktree. It is
supervised, so it survives a restart. It has a memory, so it knows what happened
last week. It can hand work to another session, on another model vendor, without
you in the middle.

You talk to it from Discord, from the terminal UI that ships in the binary, or
from a gateway you write yourself.

Full documentation lives in
[Herrscherd/herrscher-docs](https://github.com/Herrscherd/herrscher-docs). Page
slugs are given below as inline code, for example `architecture/memory`.

## Install and run

Requires Go 1.25+.

```bash
git clone https://github.com/Herrscherd/herrscher.git
cd herrscher
go build -o herrscher .    # the only binary; plugins are compiled in
./herrscher init           # compose the plugin stack, seed config, choose routing
./herrscher                # serves, with the terminal UI in the foreground
```

`init` ends by asking where agent turns run: the route policy, the gateway
credentials that `gateway-only` needs, and the model a session gets when it names
none. It writes them to `.env`, and `herrscher models list` shows what the active
policy offers.

The gateway credentials are both halves or neither. A base URL without a token
would redirect the CLI while it keeps authenticating as this machine's own
account, which is the one failure mode worth refusing outright.

**The terminal path needs no Discord token.** The in-tree terminal gateway is a
first-class gateway, so you can create, drive and resume sessions from your shell
alone. A token is only required if you enable the Discord gateway. For a
boot-started service, an Arch `PKGBUILD` and the rest, see `guide/installation`
and `guide/service`.

Installed as a service, the daemon keeps the `PATH` of the shell that installed
it: a launcher supplies almost nothing, and an agent cannot use a tool it cannot
find. It drops the entries that cannot outlive that shell, because a service
resolving commands through a directory anyone can recreate would trust whoever
got there first.

### Starting work

```bash
./herrscher "read the thread and propose a split"   # a session on that task
./herrscher --print "…"                             # one turn, the reply on stdout
```

An argument that carries whitespace is a task, not a verb. It opens a session of
its own (isolated worktree, terminal-only, named after the task's opening words),
sends the task there, and opens the UI on that tab so the turn runs in front of
you and you can answer it. The session is persistent either way, and closing the
window does not end it: `session list` shows it, `session seed --name` continues
it, `session close` ends it.

`--print` is the non-interactive form: one turn, the reply on stdout and the
session name on stderr, so `herrscher --print "…" > out.md` holds the answer
alone. A run with no terminal takes that form on its own, since there is nothing
there to draw a window on.

A single unrecognised word stays a verb, so a typo is still a typo. Use `-p` to
force a one-word task.

### A bare `herrscher`

A bare `herrscher` starts something rather than resuming it. Every launch opens
on a conversation of its own, named after the directory you launched in
(`herrscher-2` if that name is taken, a random `s-xxxx` if the directory cannot
be one). What came before is still there: a live tab if it is still running,
`/resume` otherwise. Nothing piles up either, since a session nobody spoke in is
archived when the window closes.

One daemon per state file. A second one over the same state answers every message
twice and bills it twice. So a bare `herrscher` on a TTY **serves when nothing
else is** and **attaches** when the service already is, driving the same UI over
the sockets the running daemon exposes. You see the agent that is actually live,
and quitting the window leaves that daemon running. Off a TTY there is nothing to
attach to, so the second daemon is refused and the process holding the state is
named. Use `--state` if you really want a separate world.

## What a session gives you

### Durable sessions

Each session is a channel, an agent and an isolated git worktree, supervised and
restarted automatically. The conversation resumes from the backend's own resume
token, so a restart is not a new conversation.

A session can be created explicitly or adopt an existing one with `--channel_id`.
On Discord it opens the first time its owner pings the bot in a channel. A new
session lands in the daemon's home unless `--under` names another category or
forum, so work opened from one server does not surface in another.

→ `architecture/durable-agents`, `architecture/session-lifecycle`

### Memory

An Obsidian vault, provisioned for you, scoped shared-per-project and
private-per-agent, and recalled before every turn.

→ `architecture/memory`

### Learning

Opt-in consolidation of a session's work into scoped nodes, with aging, semantic
merge, cross-agent promotion, and archiving you can undo.

→ `architecture/learning`

### Skills the agent writes for itself

An agent that just spent three turns working out a procedure writes it down with
`<skill name="…">`, and finds it again next session as an ordinary skill: in the
menu, expanded on demand, beside the ones the repository committed.

The node is the truth and the disk is a projection, which hands the skill aging,
semantic merge and reversible archiving without one rule of its own. Use is what
keeps it alive: the engine reports which skills a turn activated, that refreshes
their date, and the sweep archives the one nobody reaches for.

Two boundaries matter here. The rendering lives in a root of its own and never in
`.claude/skills`, because it deletes what it no longer projects and must not be
able to delete a file someone wrote by hand. And a repository skill always beats
a learned one.

A skill writes and revises itself freely in its agent's private scope, but
crosses to the shared project scope only on a `herrscher skill approve`. Its body
comes from the journal, which carries chat messages and web pages, and promotion
is the one place where the blast radius goes from what this agent believes to
what every agent executes.

Off by default: `MEMORY_LEARNED_SKILLS`.

→ `architecture/learning`

### Delegation across vendors

An agent ends a turn with a `⟢` trailer to delegate, fan out, route, merge or
hand off. Workers inherit their agent's vendor, so one run can mix Claude, Codex
and Cursor.

→ `architecture/coordination`, `reference/trailers`

### Agents that start their own turns

A schedule wakes a session on a cadence with nobody in the room.

```bash
herrscher schedule add --name digest --agent scout \
  --cron '0 9 * * 1-5' --task "read yesterday's PRs"
```

That hands the task to the session every weekday at nine, and `--every 30m` does
the same on a fixed period.

An `--agent` target owns exactly one session, named after the schedule and
reused, so a daily job leaves one worktree behind and not three hundred and
sixty-five. A `--session` target names a session you already have, and a window
whose session is gone is skipped rather than served by a session invented in its
place.

The turn enters the same queue a human message does, so it crosses the budget
gate, memory, skills and the fan-out to the gateways with no code of its own. A
schedule never has two turns in flight: a cadence faster than a turn degrades to
as often as it can, instead of building a queue with no bottom.

Windows missed while the daemon was down fire once on the next start if they fall
inside a grace period (one hour by default), and the turn is told in its own
prompt that it is late and by how much, so a reply written at 09:45 does not
claim to be the 09:00 one.

`schedule list` says the next window for each, `pause` and `resume` stop and
restart a cadence without losing its history, and `schedule run` fires one by
hand without moving it. That last one is how you read a task's first reply before
letting it run unattended.

→ `architecture/proactive`

### Sessions that run on another machine

```bash
herrscher host add build1 --ssh me@build1 --workspace /srv/work
herrscher session create --name x --host build1
```

The whole session lives over there: its worktree, its agent files, its backend
process. An agent can carry a default host, so a fleet member always lands on the
machine its work belongs to.

Registering provisions. Herrscher reads the target's platform, cross-compiles a
binary for it, copies it into `~/.herrscher/bin`, and records the version it put
there. There is nothing to install by hand and nothing to keep in step.

The daemon stays the only one holding state, and the remote side is a runner
rather than a second daemon. The session's control socket and this daemon's
command socket are forwarded back over the same ssh connection, so the bridge
over there dials a unix socket exactly as it does here, and `herrscher <verb>` in
the agent's own shell reaches the daemon that started it. Each session forwards
its own path and each launch owns its connection, so a second session on the same
host cannot take the first one's socket away.

Three refusals replace a blind start: a host that was never provisioned, a host
running a different build than this daemon would ship, and `--clone` combined
with `--host`, since the forge client runs here and would clone onto the wrong
machine. `host check build1` says in four lines whether ssh, the binary, the
workspace and git are there.

→ `architecture/remote`

### Tool calls that answer to a policy, and to you

An agent session's Claude Code tool calls are decided before they run. A rule
reads `allow Read`, `ask Bash(git push*)` or `deny Write(/etc/*)`, the strictest
match wins, and an agent's own `APPROVALS` file can only tighten what the daemon
set, never widen it.

On `ask` the question goes to the session's own gateways as a two-button menu and
the tool call waits. An answer settles it, and nobody answering within the
timeout is a refusal, said to the model in words it can act on.

A session is `ask`, `strict` (everything a rule does not name is asked about too)
or `bypass` (no hook at all).

It is wired in by materializing a `PreToolUse` hook into the session's worktree,
so it binds the agent herrscher started and nothing else on the machine. That is
also the whole of its reach, and both limits are worth knowing before you rely on
it. The hook rides the agent files, so a session created without `--agent` has no
hook and no policy, and asking for a mode there is answered with a warning saying
exactly that. And it is written into `.claude/settings.json` alone, so a Codex or
Cursor session in the same run is not covered.

Every failure of the hook allows and says why. This is a guardrail against
mistakes, not a sandbox, and a daemon that has gone away must not stop a `claude`
run by hand.

`approve list`, `approve allow <id>`, `approve deny <id> --reason '…'`,
`approve rule` and `approve mode` are the whole surface. With no rule configured
nothing is ever asked, which is exactly how herrscher behaved before this
existed.

→ `architecture/approvals`

### A daemon that knows who is calling

Identity comes from the entry point, never from the message.

A session talks to its own command socket, not the operator's, so it can no
longer widen the policy that decides its own tool calls, answer its own approval
requests, or speak in another session's name. It cannot open a session either,
which is the same refusal wearing a different hat: a session created from the
command line carries whatever flags the command line chose, and one with no agent
or `--approvals bypass` is a second session where the caller's own policy does
not apply. Delegation is untouched, because a `⟢ delegate:` trailer opens a
worker that runs a registered agent and answers to a policy the way its lead
does.

That much applies from the upgrade, with nothing to configure.

For humans, nothing changes while nobody holds a role. Each gateway keeps its own
rules and herrscher decides nothing. `role grant <principal> <owner|operator|observer>`
hands the decision to the core, and says so plainly the first time, because from
then on every gateway account without a role is `observer`. `role list`,
`role show` and `role revoke` are the rest of the surface.

Locally this is a guardrail rather than a wall: the agent and the daemon run
under one account, so nothing stops a determined agent from looking for the
operator's socket in the same directory. The real boundary is a session on a
host, under a restricted account, where ssh forwards that session's socket and no
other.

→ `architecture/authorization`

### An agent that knows what it is running inside

Every turn's prompt carries a `<capabilities>` block: that this session is
supervised by a running daemon, that the daemon is reachable from its own shell
as `herrscher <verb>` with the credentials it already holds, and the list of
verbs *that session* may run, one line per family.

The list comes from the daemon's registry at spawn, over the environment beside
the gateway credentials, because a gateway's contributed verbs exist only where
that gateway is instantiated. So the block cannot advertise less than the daemon
actually answers.

It is filtered to what the daemon will accept from a session, for a reason worth
stating: a model told it can run `approve rule` and then refused does not read
the refusal as an answer, it reads it as a fault to work around.

Without the block, an agent asked to read a chat channel reaches for a token of
its own and concludes the platform is unreachable, while the daemon behind it
holds that gateway open.

### An agent that knows who it is working for

Every turn's prompt also carries a `<user>` block: the name, commit email and
GitHub handle that git on this machine already holds, read with `git config --get`
from the session's worktree. A per-repository identity is honoured exactly as it
would be on a commit, and nothing has to be configured twice.

There is no setting for it and no file to fill in. An operator who has told git
who they are has told herrscher. A machine where git says nothing adds no block
at all rather than a placeholder, and git is never asked more than once per
session.

The block says in its own words that it is what a commit here would be signed
with and not a proof of identity, because the value comes from a file anyone with
the checkout can edit.

`herrscher whoami` prints what was resolved and which git key each part came
from, and answers without a daemon, because a diagnostic must not sit behind the
thing it is used to diagnose.

### Model routing

Each backend declares the models it offers in its manifest, so the catalog is
queryable without instantiating anything. A route policy picks which of them a
build may use. `all` runs on this machine's own vendor logins. `gateway-only`
runs every turn through a gateway you supply, and refuses the two ways around the
catalog: a free-form `--cmd`, or a session with no model at all.

### Also

Cross-backend skills, where one `SKILL.md` works on every backend. That includes
the playbooks the binary ships with and installs into `~/.claude/skills`, kept
current, because a playbook is instructions and a stale one is the agent
following last release's rules. A copy still bytewise the one we wrote is
replaced when the shipped text changes. A copy you edited is left alone and named
at startup, so you learn the rewrite is not in effect instead of finding your
version gone. See `guide/skills`.

And per-node memory budgets, in `guide/budgets`.

## The terminal UI

Discord, the in-tree terminal UI, or your own gateway plugin all sit behind the
same neutral port. The UI is the one that ships in the binary.

→ `plugins/gateway`, `plugins/terminal`

### Sessions as tabs

Several sessions render as live tabs. Each answer opens with a titled rule, so a
turn keeps its shape when colour does not survive, and nothing else is ruled,
since a frame drawn around every turn marks none of them.

Events are typed by role (reasoning, tool family, notice, error). The agent's
markdown and diffs are rendered, a resumed session's history is replayed as the
turns it was, and the status bar carries the session's cumulative cost and a
gauge of how full its context window is. `/usage` spells that out.

The `/` palette is derived from the daemon's own registry, via `commands --json`
filtered to the verbs a tab may run, so the menu cannot fall behind what the
daemon dispatches. It scrolls rather than truncating, so every verb is reachable
with the arrows.

An attached window follows the live turn, because every driven session taps the
daemon's event socket. Esc stops a turn as an interruption rather than an error,
keeping whatever the agent had already written.

Picking up a chat session's tab and typing there answers you *there*. A turn says
where it was said, so it is rendered in the window that asked and not posted a
second time in the channel the conversation came from, where nobody asked
anything.

### Using the terminal you are in

Capabilities are probed once at startup, and every feature that has a richer and
a plainer form reads that one answer.

Images render through the kitty graphics protocol where it exists, sixel where it
does not, and Unicode half-blocks everywhere else, so a picture is shown on any
terminal that can draw text. PNG, JPEG, GIF and WebP all decode, and an oversized
one is downscaled rather than dropped. Links the transcript already contained
become real OSC 8 hyperlinks where the terminal has them, and styled text where
it does not.

Degradation is silent at the point of use: nothing announces that a protocol is
missing, it simply draws the next best thing. `/capabilities` says what is live,
what is not, and what each unavailable feature falls back to.

### Nothing opens on its own

A link is followed because the operator asked, never because a message arrived or
a turn completed.

`ctrl+l` walks the links in the transcript. While one is selected the status line
shows its *resolved target*, since a markdown label can name one destination and
point at another, and `ctrl+o` opens it: a URL through the desktop handler, a
`file.go:42` reference through `$EDITOR` at that line. That is display, not a
confirmation prompt. The keypress is the consent.

### Reading a long session

`ctrl+s` searches the transcript as you type, marks every match, cycles them with
`ctrl+n` and `ctrl+p`, and puts the scroll position back where it was when you
close it.

`alt+↑` and `alt+↓` jump by turn. `ctrl+t` collapses the history above the
current exchange to one line per turn, `ctrl+f` folds long code blocks to a
summary line, and `ctrl+y` copies the last one to the clipboard as raw text.

Tool lines carry a budget of two rows apiece. Unbounded, a compound shell command
becomes a paragraph in a single colour, and the session path it opens with is
identical on every call. So a continuation hangs under the target it belongs to,
an absolute path deep enough to cost a row is said in the two segments that
identify it, and what is left over is counted rather than dropped: `+3 ↵`.
`alt+e` lifts the budget and spells every command out as it was run.

Diffs are coloured by hunk, and by the basic ANSI pair on a 16-colour terminal,
where a hex green would be approximated away. Tables truncate their widest column
instead of wrapping into confetti.

→ `reference/keybindings`

### Getting text back out

The window runs on the alternate screen with the mouse captured. That is what
lets the wheel scroll a transcript the terminal keeps no scrollback for, and it
is also why click-drag selection is unavailable exactly where the interesting
text is. So there are two ways out.

`alt+y` puts the last answer on the clipboard, and `/copy [reply|turn|all]` takes
the answer, the whole exchange that produced it, or the tab's transcript. What
was said only, never this window's tool lines, reasoning, costs or notices, which
are noise in a paste.

`ctrl+g` (or `/mouse`) hands the mouse back to the terminal so its own selection
works as always. The status bar says `mouse → terminal` while it is released,
`pgup` and `pgdn` still scroll, and `ctrl+g` takes it back.

### Numbers that mean what they say

The context gauge measures the live prompt, read from the counters that arrive
mid-turn. The reply that ends a turn carries the vendor's *billing* totals for
it, which is what the cost line and the budget need, and what the window would
otherwise have shown as an impossible `1252.2k/200k · 100%`.

## Plugins

Plugins compile **into** the binary, the xcaddy pattern: add a blank import,
rebuild. `herrscher update` bumps every unpinned compiled-in plugin, rebuilds and
reinstalls the binary. Restart the service afterwards to run it.

| Category | Port | Official plugin |
|----------|------|-----------------|
| Gateway | `Gateway` | [herrscher-discord-gateway], in-tree `terminal` |
| Backend | `Backend` | [herrscher-claude-backend], [herrscher-codex-backend], [herrscher-cursor-backend] |
| Memory | `Memory` | [herrscher-obsidian-memory] |
| Orchestrator | `Orchestrator` | [herrscher-orchestrator] |
| Skills | playbooks only | [herrscher-superset-skills] |

### Versions and pins

Each plugin carries its own version. `herrscher plugin list` reports, per module,
the version installed, whether it is pinned, and the newest published. That last
one needs the network and reads `?` without it.

```bash
herrscher plugin add <module>@<version>   # install a chosen version
herrscher plugin pin <module> [<version>] # move there first if given, then pin
herrscher plugin unpin <module>           # drop the record
```

`herrscher update` skips every pinned module and names each one it skipped, so a
pin never looks like a silent no-op. The pins live in `.herrscher-pins` beside
`plugins.go`: one module path per line, `#` comments ignored, no versions, since
`go.mod` already holds those and a second copy could disagree.

### Every change is a transaction

Before writing, the CLI reports what can be known against the change (that it
moves a module backwards, or that it wants a different `herrscher-contracts` than
the composition resolves) and asks to proceed. It then saves `go.mod`, `go.sum`
and `plugins.go`, and runs `go get`, `go mod tidy`, `go build ./...`,
`go install .`.

When the build refuses, it prints the compiler's own error and offers two
outcomes: restore the three files and leave the tree exactly as it was, or keep
it as it is to repair by hand. A run with `--yes`, or with no terminal to ask,
takes the safe branch at both. It proceeds, and it restores.

The same flow is available from the UI's `/plugins` screen, where both questions
are select menus and a successful change ends on a line saying it applies at the
next restart.

→ `guide/managing-plugins`

### Contributed commands and skills

A plugin may also contribute commands of its own and the skills that teach an
agent to use them. The daemon namespaces each contributed verb under the plugin's
kind, so the Discord gateway declares `channel read` and an operator types
`discord channel read`. The prefix is imposed by the host, and two plugins can
never collide.

A plugin's skills install only when that plugin is in the build, so a Discord
playbook never sits in the context of a machine that has no Discord. Skills are a
category of their own too: a plugin may contribute nothing but playbooks, and it
is added, listed, pinned and removed by the same commands as any other.

What a plugin hands over is decided at startup rather than at compile time. A
factory that looks at the machine and finds the tool its playbook describes is
not installed returns nothing, and the daemon says which plugin declined and why.

→ `plugins/model`, `plugins/writing`

## Architecture

Hexagonal.
[herrscher-contracts](https://github.com/Herrscherd/herrscher-contracts) is the
authority: it declares the ports and the neutral types, with zero platform
mechanics. Every dependency arrow points inward at it. The agnostic domain
(`core`) depends on no edge, the edges depend on no core, and only the host's
wiring file ever sees two concrete types at once.

Purity tests (`TestHostPurity`, `TestCorePurity`,
`TestCoreNamesNoConcretePlatform`) fail the build if a concrete platform leaks
in.

→ `architecture/hexagonal`

## Documentation

Full docs:
[Herrscherd/herrscher-docs](https://github.com/Herrscherd/herrscher-docs). Good
places to start are `overview/introduction`, `architecture/hexagonal`,
`plugins/model`, `guide/quickstart` and `reference/env`.

## A note on history

The platform grew out of a Go monolith that bridged Discord to a local Claude.
Herrscher is that monolith decomposed along its natural seams (channel, model,
domain) so each can evolve independently. The contract shapes were chosen
deliberately to make the eventual transport change, in-process to NATS/gRPC, a
detail rather than a rewrite.

[herrscher-discord-gateway]: https://github.com/Herrscherd/herrscher-discord-gateway
[herrscher-claude-backend]: https://github.com/Herrscherd/herrscher-claude-backend
[herrscher-codex-backend]: https://github.com/Herrscherd/herrscher-codex-backend
[herrscher-cursor-backend]: https://github.com/Herrscherd/herrscher-cursor-backend
[herrscher-obsidian-memory]: https://github.com/Herrscherd/herrscher-obsidian-memory
[herrscher-orchestrator]: https://github.com/Herrscherd/herrscher-orchestrator
[herrscher-superset-skills]: https://github.com/Herrscherd/herrscher-superset-skills
