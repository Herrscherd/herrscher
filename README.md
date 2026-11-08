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
the sockets the running daemon exposes. You see the agent that is actually live.
Quitting the window has two outcomes. After an attach, the daemon you found keeps
running. After a serve, quitting stops that daemon, and the turns still running
in it are cancelled. Off a TTY there is nothing to
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

### Plugins on another machine

A plugin category can run in a plugin-host on another machine. Name the category
in `HERRSCHER_REMOTE` and point `HERRSCHER_NATS` at the shared NATS server. Two
settings guard it:

- `HERRSCHER_ANNOUNCE_KEY` authenticates the announcements a plugin-host publishes.
  Every host and plugin-host that exchanges them must hold the same secret of at
  least 32 random bytes, and a host with a remote category refuses to start
  without a valid one. The key signs announcements only: it neither encrypts the
  traffic nor grants a caller any right. The hub, its plugin-hosts and its bridges
  receive it. It is kept out of the environment the agent's vendor CLIs inherit,
  but processes running as the same user can still read it, from the `.env` file
  or from `/proc`, so the boundary is the user account.
- The gRPC connections use mTLS (`HERRSCHER_TLS_CA`, `HERRSCHER_TLS_CERT`,
  `HERRSCHER_TLS_KEY`). With mTLS configured, the host dials any address an
  authenticated announcement advertises. Without it, the host dials loopback
  addresses only, so plaintext traffic never leaves the machine.

### Tool calls that answer to a policy, and to you

A session's tool calls are decided before they run. A rule
reads `allow Read`, `ask Bash(git push*)` or `deny Write(/etc/*)`, the strictest
match wins, and an agent's own `APPROVALS` file can only tighten what the daemon
set, never widen it.

In an `allow` rule on a shell command, `*` and `?` never cover a shell operator
(`; & | ` $ < > ( )`) or a line break, so `allow Bash(git push*)` passes
`git push origin feat/x` but not `git push && rm -rf ~`, `git push; curl x|sh`
or `git push $(id)`. An operator written out in the rule matches literally:
`allow Bash(make build && make test)` covers exactly that command. `deny` and
`ask` rules keep their wildcards unrestricted, so `deny Bash(rm -rf /*)` also
blocks `rm -rf /x; ls`.

That restriction closes shell operators only. It does not stop a program that an
argument runs: `allow Bash(git *)` still passes `git -c core.pager='sh -c id' log`,
and `allow Bash(find *)` still passes `find . -exec id {} +`. The same holds for
`sh -c`, `bash -c`, `npm run`, `make`, and `xargs`. For those, write a targeted
deny, which has no operator restriction and wins over any allow:
`deny Bash(git -c*)` refuses the `-c` form while `allow Bash(git *)` keeps
passing the other git calls.

On `ask` the question goes to the session's own gateways as a menu of four
answers, and the tool call waits. The four are:

- `allow`: this call runs, and the next identical one asks again.
- `allow --session`: this call runs, and the same call passes for the rest of
  the session. The grant lives with the session and ends when it stops.
- `allow --always`: this call runs, and a rule is written the way `approve rule`
  writes one, so it keeps passing after restarts. The rule names the exact
  subject as it ran (`Bash(git push origin feat/x)` for a shell command), and
  the menu shows the rule before you press it. A subject holding a wildcard, a
  shell operator or a line break gets no rule, and the operator is told so.
- `deny`, optionally with `--reason`: the call is refused, and the reason is
  what the agent reads in its refusal.

Nobody answering within the approval wait, 5 minutes by default, is a refusal,
said to the model in words it can act on. The gateways are told the deadline,
so a menu can show when the question expires.

A session is `ask`, `accept-edits`, `plan`, `auto` (everything passes except what a
deny rule refuses, the hook stays installed), `strict` (everything a rule does not
name is asked about too), `smart`, or `bypass` (no hook at all).

`smart` lets a model rate each call that no rule settles, except reads (`Read`,
`Glob`, `Grep`, `LS`), which pass as before. A `low` rating passes the call,
`medium` and `high` ask you as `ask` does. The rating is kept for the session,
so the same subject is not rated twice. The model is the first installed
LLM backend, the one the memory extractor uses, and `HERRSCHER_CURATION_MODEL`
overrides its model. Without any LLM backend, or when
the rating fails, smart decides as `ask` does and says so to the operator: a call
no rule names runs unasked, a call a rule asks about is put to you. Smart costs
one model call per distinct unmatched subject.

How it is wired in depends on the backend, and each backend says for itself what
it can enforce. Claude Code gets a `PreToolUse` hook materialized into the
session's worktree, with or without an agent: a session created without `--agent`
now gets one written into `.claude/settings.local.json`, which is local to the
worktree and never touches a tracked settings file. Codex is asked over the
app-server, which requests an approval per command and per patch, so the same
rules bite there. Cursor declares that it enforces nothing, because `cursor-agent`
exposes no hook and no approval channel.

That last one is the shape of every limit here: a mode that will not bite is said
out loud at create time rather than discovered on the first tool call nobody was
asked about. A shared session (no isolated worktree), a remote session without an
agent (no channel to materialize a hook through), a backend that cannot enforce,
and a daemon that cannot tell what its backends do all produce a warning naming
the session and the reason. The session is still created, and it still records
the mode: what is refused is the silence, not the session. `session switch` says
the same thing when it moves a gated session onto a backend that cannot enforce
its mode.

The hook binds the agent herrscher started and nothing else on the machine, which
is both its guarantee and the whole of its reach.

Every failure allows and says why, on both paths. This is a guardrail against
mistakes, not a sandbox, and a daemon that has gone away must not stop a `claude`
run by hand.

`approve list`, `approve allow <id> [--session | --always]`,
`approve deny <id> [--reason '…']`, `approve rule`, `approve ask` and
`approve mode` (`ask`, `bypass`, `strict` or `smart`) are the whole surface.
Flags come after the id or before it: `approve allow --session <id>` works too.
With no rule configured nothing is ever asked, which is exactly how herrscher
behaved before this existed.

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

What the agent's shell can read is narrower than what the daemon holds. Every key
a gateway's manifest declares (`DISCORD_BOT_TOKEN` and the rest) is read once,
kept inside the daemon, and removed from the process environment, at startup and
again on every `--env-file` load. So `env` in a session shows nothing of it, and
no vendor CLI a session spawns inherits it. The gateways still build from those
values and your `.env` is untouched: what changes is who gets to read it. Keys no
gateway declares, your `ANTHROPIC_API_KEY` among them, stay where they are,
because the agent needs them to run at all.

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

### Health, one command away

`herrscher health` asks the running daemon for a snapshot of everything it depends
on, and prints it as a table grouped by what the checks cover: the daemon and its
key proxy, the gateways, the backends with each account and profile, the
compaction stages, the memory plugins and the key pools. Each line says the state
(ok, degraded, down or off), how long the probe took, and the facts the host knows
about the service.

A gateway the daemon does not run reads as off, and so does a memory plugin that is
not the one in use: the first local memory plugin with a factory is the live one,
so in remote mode (`HERRSCHER_REMOTE=memory`) the local memories show off.

`herrscher health <service>` shows one service, or every account of it, with its
cause and the remedy when there is one, for instance the login command to run.
A service is named by its plugin, or by `plugin:account` for one account of a
backend. `--json` prints the same snapshot as structured data for a script.

Each probe runs under a bounded pool with a five second limit. `--deep` lifts the
limit to thirty seconds and asks the plugins that support it for a real round trip
rather than a local check, which costs requests, so it is opt-in. `--watch`
refreshes the table every fifteen seconds until interrupted, and `--quiet` prints
nothing and only sets the exit code.

The exit code tells a script the outcome: `0` when nothing is degraded or down,
`2` when the worst state is degraded, and `1` when something is down or the
daemon cannot be reached. An unreachable daemon is reported on stderr with the
socket it was looked for, so a stopped daemon reads as a finding rather than a
silent failure.

### Model routing

Each backend declares the models it offers in its manifest, so the catalog is
queryable without instantiating anything. A route policy picks which of them a
build may use. `all` runs on this machine's own vendor logins. `gateway-only`
runs every turn through a gateway you supply, and refuses the two ways around the
catalog: a free-form `--cmd`, or a session with no model at all.

When a session names no model, the first of these that fits applies:

1. the session's own `--model`;
2. the agent's model, read from the `model` file in its home;
3. the vendor's default, from `HERRSCHER_DEFAULT_MODEL_<VENDOR>` (for example `HERRSCHER_DEFAULT_MODEL_CLAUDE`);
4. the global default, from `HERRSCHER_DEFAULT_MODEL`;
5. the backend's own default, when none of the above is set.

A fallback that belongs to another vendor than the one requested is skipped, and
logged unless it is the global default. An explicit `--model` that the catalog
refuses is an error.

Backends that can list their models are asked at startup, in the background, and
their answers are cached on disk. Discovered models are added to the static
catalog; they never remove one. `herrscher models list` shows where each model
comes from (`catalog` or `discovered`), and `herrscher models refresh` asks again
right away. Both take `--state FILE`, the daemon state file whose directory holds the
cache; without it they use the daemon default, the same cache the daemon reads.

A model that a backend declares `Hidden`, or that `HERRSCHER_MODELS_HIDDEN` names, is
left out of the selector and of `herrscher models list`, but it still resolves: a
session recorded on it resumes it, and an explicit `--model` naming it works. Each
time the host resolves such a model it logs a warning, and it never switches to
another model in its place.

`herrscher models list --json` gives each model its prices and its window. `--all`
also lists the hidden models, marked `hidden: true`, which still resolve when requested;
without it the output is unchanged. Prices
are in USD per million tokens (`inputPrice`, `outputPrice`, `cacheReadPrice`,
`cacheWritePrice`), and a price of 0 means the price is unknown, never free.
`contextWindow` is the model's window in tokens, 0 when its backend declares none.
A turn's cost is the one its backend reports. When the backend reports none, the host
estimates it from the turn's tokens and the resolved model's prices: uncached input,
output, cache read and cache write, each times its price, summed and divided by
1000000. The estimate is stored as the turn's cost, so the transcript, the cost line
and `CostCap` all count it. It is computed only when every category the turn used has
a known price: a turn that uses a category priced 0 keeps no estimate and shows no
cost, so a partial figure is never shown as a cost. A gateway route has no prices and
gets no estimate. Claude reports its cost on each turn's result, so the estimate is a
fallback for it. Claude's `input_tokens` leaves out cache, which is why the estimate
prices cache separately. Codex and cursor report neither token counts nor a cost, so
their turns show no cost.

| Variable | Default | Effect |
|---|---|---|
| `HERRSCHER_MODELS_DISCOVER` | every backend | comma-separated vendors that may be asked (`claude,codex`), or `none`; with `HERRSCHER_ROUTE_POLICY=gateway-only` vendors are asked only through the gateway, and only when its credentials are set |
| `HERRSCHER_MODELS_TTL` | `24h` | how long a cached list is kept before it is asked again |
| `HERRSCHER_MODELS_TIMEOUT` | `15s` | longest wait for one vendor's answer |
| `HERRSCHER_MODELS_HIDDEN` | none | comma-separated model IDs hidden from the list and the selector; a session already on one still resumes it, with a warning |
| `HERRSCHER_DEFAULT_MODEL_<VENDOR>` | none | default model of one vendor |

The cache is `models-discovered.json`, next to the daemon state file: the directory of
`--state` when given, otherwise `HERRSCHER_STATE_DIR`, otherwise `~/.config/herrscher`.
It is created on first write, readable by its owner only. Writers take an exclusive lock
(`flock` on Unix, `LockFileEx` on Windows), so two writers never drop each other's vendor.
A vendor that fails or stays silent past its timeout keeps its last known list, and the
daemon logs a warning. The daemon never waits for discovery to start.
A bridge started by a local daemon receives that daemon's `--state` and reads its cache; a
remote bridge reads the cache of its own machine and receives no `--state`. A clash warning
is logged once per model ID for the life of the process, and each bridge keeps one
model discovery for its whole life. Under `gateway-only` the host drops every native answer
whatever the backend returned; a backend that ignores the policy would still launch its own
CLI, which only that backend's gate prevents.

### Also

Cross-backend skills, where one `SKILL.md` works on every backend. The playbooks
come from the plugins in the build and are installed into `~/.claude/skills`,
kept current, because a playbook is instructions and a stale one is the agent
following last release's rules. A copy still bytewise the one we wrote is
replaced when the shipped text changes, and removed when no build ships it any
more. A copy you edited is left alone and named at startup, so you learn the
rewrite is not in effect instead of finding your version gone. See
`guide/skills`.

And per-node memory budgets, in `guide/budgets`.

## The terminal UI

Discord, the in-tree terminal UI, or your own gateway plugin all sit behind the
same neutral port. The UI is the one that ships in the binary.

→ `plugins/gateway`, `plugins/terminal`

### The shape of the window

A rail across the top opens with the name `herrscher`, then lists the open sessions: the active
one is marked `▸` and underlined, a working one shows a spinner, one waiting for
approval shows `!`, a finished one `✓`, and an unread one `●` with its count of new
entries. The hints `^P agents · ^T onglet` sit at the right, and a rule runs under the
row. When the sessions do not fit, names shorten first, then the rest folds into a
`+N` count, and the active session always stays visible.

At 110 columns and wider a side column runs down the right of the transcript,
separated by a vertical rule. It shows the session (project, branch, worktree,
gateways), the todos with their `n/m` count, the sub-agents still running and those
just finished, background tasks when the backend reports them, the files the session wrote with their `+added −removed` line counts
against git, and the context gauge (24 cells) with the compaction threshold (92 %). `ctrl+x v` hides or
shows it. Under 24 rows the bottom sections fold to their summary line. The
transcript keeps the full width below 110 columns, and the todos and sub-agents
panels stay in the stack above the composer whenever the column is hidden.

The composer sits in a rounded box at the foot, and the status line is glued right
under that box, so the two things you act on are one object rather than two rows
separated by a rule.

`/resume` filters as you type (a fuzzy match on name, project, vendor and gateway)
and shows the last lines of the selected session above the list.

Every list the window opens (the `/` palette, `@` completion, `/resume`,
`/session switch`, `/skills`) shares one grammar: unbordered rows floating just
above the box, the selected one on a filled background, a footer that says what
the keys do and how much of the list is out of sight. The box is drawn with
rounded runes where the locale says UTF-8, plain ASCII otherwise, and it collapses
below 60 columns.

The window speaks French.

### Sessions as tabs

Several sessions render as live tabs. Each answer opens on a `●` gutter and each
message you send on a `┃` bar, so a turn keeps its shape when colour does not
survive, and nothing is framed, since a frame drawn around every turn marks none
of them.

Events are typed by role (reasoning, tool family, notice, error). The agent's
markdown and diffs are rendered, a resumed session's history is replayed as the
turns it was. The status bar leads with the mode as an inverse chip (Manuel, Édits
acceptés, Plan, Auto in red), then names what the session runs on (vendor, model,
reasoning effort), its project and branch, and how much room is left before the
backend compacts the conversation: a gauge that turns amber at 80 % and red at 95 %,
with the figures `41%  82k/200k`. Cost (`$0.00` until the first turn) and queued messages follow, and the fields
drop by priority as the window narrows. `/usage` spells the numbers out.

`/statusline` opens the bar's settings: tick the fields, reorder them with shift+↑↓,
and see the result in a live preview. Beside the list, a pane shows the script
contract: the JSON it receives on stdin and the `[statusline]` keys. `s` switches
between the built-in bar and the script. In `~/.config/herrscher/terminal.toml`, a
`[statusline]` section can replace the bar with a script (`source = "script"`,
`command`, `refresh`): it receives the state as JSON on stdin and its first line
becomes the bar. A script that fails or runs past one second leaves the built-in bar
with a discreet ✗.

A session has one of four modes, cycled with shift+tab or set with `/mode
[manuel|edits|plan|auto]`. The strip under the plan box shows them with the
current one highlighted, a left click on a segment switches to it, and the status
bar opens with the mode. A tab starts from the mode the session has stored
(`session get-mode`). The TUI sends the change to the daemon through
`session set-mode`, so the host enforces it: manual asks before each edit and
command, accept edits lets edits through and still asks for commands, plan is
read only and refuses writes, and auto lets everything through except what a
deny rule refuses. A refused switch is shown and the mode stays as it was. A plan reply
opens a three-choice menu: validate and switch to accept edits, validate in
manual, or keep planning. `/plan <instruction>` switches to plan and sends the
instruction.

A session's name is chosen when you know the least about what it will become, so
`/rename <nom>` changes it: the row keeps its identity and its id, the transcript
and the participants journal follow, any schedule that targets the session by
name follows too, and the bridge restarts under the new name. A session an agent
schedule owns carries a name derived from the schedule's rather than a stored
one, so renaming it says so: the next window opens a fresh session under the old
name.

The `/` palette is derived from the daemon's own registry, via `commands --json`
filtered to the verbs a tab may run, so the menu cannot fall behind what the
daemon dispatches. It scrolls rather than truncating, and its height follows the
window's, so a tall terminal shows a menu rather than a slit. Arrows walk it,
`⇞`/`⇟` page through it and `début`/`fin` jump to its ends, in the palette and in
every other floating list.

An attached window follows the live turn, because every driven session taps the
daemon's event socket. Esc stops a turn as an interruption rather than an error,
keeping whatever the agent had already written.

### Health in the window

`/health` opens the same snapshot as an overlay, and the status line carries a dot
for the worst state of the checks with `santé n/m` beside it, where `n` counts the
checks that are ok and `m` the ones that can fail. The figures refresh every fifteen
seconds while the overlay is open, and stay as they were once it is closed. Inside
the overlay `r` probes again, `d` runs one deep probe (it costs round trips) and the
refresh after it is a plain probe again, `enter` shows the cause and remedy of the
degraded and down checks, and `esc` closes it. The overlay is also in the command
palette.

### Context, and what fills it

`/context` opens the window's room: the total used against the session's window,
and a line for each part that fills it (system prompt, tools and MCP servers,
memory, skills, files read, conversation), with what is free. Only the total is
known: the backends report one count of input tokens per turn, not a breakdown,
so each part reads `n/a` until a backend reports it. Without any count yet, the
total reads `n/a` too, and no percentage is invented. Below 80 columns the bars
go and the overlay becomes a plain list. `esc` or `z` closes it, `k` re-reads the
figures.

`/compact`, or `c` inside `/context`, asks the session to compact its conversation
through the host (`herrscher session compact --name <name> [--instruction <text>]`).
The compaction goes through the session's stack, queued after a turn in progress.
A session whose backend does not declare `Capabilities.Compactor` is refused before
anything is sent. The transcript then records the window before and after, as the
backend reports it, and the gauge follows the new figure. A compaction the backend
does not report on reads `n/a` after.

Automatic compaction runs when the last measured usage reaches the session's
threshold, a percentage of its context window. The default is 92, set with
`compactAt` in `config.json` (0 disables it, values above 100 or below 0 are
refused at load). Each session can override it with
`herrscher session set-compact --name <name> --percent <percent>` (an empty percent
restores the default, 0 disables it for that session). The window size comes from
the session's model and vendor, in this order: `HERRSCHER_CONTEXT_LIMIT` when it is a positive number,
then the `contextLimits` table of `config.json` (a vendor name to a window in tokens,
for example `{"contextLimits": {"codex": 400000}}`), then the window the resolved model declares (its `contextWindow`, when it is positive),
then the built-in size of 200000 for `claude`, `cursor` and `codex`, for a model that
declares none. `HERRSCHER_CONTEXT_LIMIT` and `contextLimits` are settings you make on
purpose, so they take precedence over the window the catalogue declares for the model.
Vendor names are case-insensitive. `config.json`
refuses a zero or negative size, an empty vendor name and one vendor listed twice in
two spellings. `herrscher serve` warns once at start about a vendor that no backend of
the daemon serves; its entry is then unused. The built-in `codex` figure is a cautious
assumption, the smallest window verified in the Codex backend catalogue (`o4-mini`),
so automatic compaction runs early rather than after the provider refuses the request;
raise it in `contextLimits` when the model in use takes more. The daemon is the only
source of the window it compacts against: each session carries the window the daemon
resolved for its model and vendor (`SessionInfo.ContextWindow`), and the TUI gauge, sidebar,
status bar and statusline read that figure. The TUI resolves the same table itself
only as a fallback, when the daemon gives no size (a daemon older than this field),
from the `config.json` of its own process and its own environment. The two can differ
when `herrscher serve -config <file>` reads another file, or when the TUI runs under an
environment that the daemon does not share; the daemon's figure is the one that counts.
An invalid `config.json` makes `herrscher serve` refuse to start, while the TUI shows a
flash naming the file and falls back to the built-in sizes, or to the
`HERRSCHER_CONTEXT_LIMIT` override when it is set. After a measured compaction still above the threshold, automatic compaction does not retrigger: it rearms once a
measured turn lands under the threshold. A compaction the backend does not measure
never retriggers. `compactStack` lists the stages in order, where `backend` is the
session's own compaction and every other name is the kind of a plugin of the
`compactor` category, built once at start through its `Compactor` factory.

The `jev` stage, from [herrscher-jev-compactor], prunes the tool calls the
assistant no longer needs before the backend summarises what is left. List it
ahead of the backend:

```json
{ "compactStack": ["jev", "backend"] }
```

It needs `TYPESAFE_API_KEY` in the environment of `herrscher serve`; the optional
`JEV_*` settings of that plugin tune it. A required setting that is missing makes
`herrscher serve` refuse to start with a message naming the variable, there is no
fallback that would drop the stage quietly. A daemon whose `compactStack` does not
name `jev` never builds it and starts without the key. The stage edits the history
only for a session whose backend declares `Capabilities.HistoryEditor`: the host
then hands plugin stages a history editor that reads and prunes the conversation
through the bridge, and the `backend` stage never receives one. The `claude`
backend declares it from herrscher-claude-backend v0.12.0, the version this host
builds with. On another backend the stage gets no editor and leaves the history as
it is. The bridge sends the history in frames of at most 128 KiB, and the host
refuses a history past 64 MiB. A read or a prune that the bridge does not answer
within two minutes fails the stage. The host sends the edits of a prune in
batches of 1000, each one applied on its own: when a later batch fails, the
batches before it stay applied. When Jev itself fails, the stage leaves a note
and the `backend` stage still runs. A backend that declares
`Capabilities.HistoryEditor` while its running instance does not implement the
port fails the stage, and the host writes one warning line to stderr the first
time it happens.

When a backend declares `Capabilities.Compactor` but its running instance does not
implement the port, the host writes one warning line to stderr the first time that
instance answers without the port, and each compaction of that session is then
refused with the same error. The host does not repair the instance. A `codex`
session in one-shot mode is such an instance: `CODEX_STREAM=false`, or
`HERRSCHER_CODEX_ONESHOT_WINDOWS=1` on Windows, runs the one-shot responder, which
has no port.

### Notifications

When a turn finishes, and when an approval waits for you, a toast appears at the
top right of the window and fades after a few seconds. It is a bordered box: the
title with the age of the event, the session with its vendor and gateways (and the
duration of the turn), the approval question when one waits, and the way out, which
is `ctrl+p` to the session. The toast only signals: it never answers an approval or
opens anything.

The window also tells the terminal, but only when the window lacks focus. The
terminal gets an OSC 9 notification (`ESC ] 9 ; text BEL`) where it is known to
handle one (iTerm2, WezTerm, ghostty), and a plain BEL everywhere else. A
terminal that reports focus lets the window know when it is in the background; a
terminal that does not report focus is signalled on every event, since nothing
says whether the window is looked at. The BEL fallback is a heuristic on the
terminal's name, not a check that the terminal rang.

`n` (with an empty composer) or `/notify` cycles which events notify: turn end
and approval, turn end only, or approval only.

### Rewinding

Before each message sent, the host takes a checkpoint of the session's worktree (a
git snapshot under `refs/herrscher/checkpoints/`, without touching the index or the
files). On an empty composer and outside a turn, `Esc Esc` opens the "Rewind"
screen: the list of messages with the files each one touched, side by side when
wide (under 80 columns, the files move below the message).

From this screen, `Enter` opens the choices: "restore the code and the conversation",
"restore the conversation only" and "restore the code only", each with a
confirmation (`y`). The conversation returns to the message by relaunching the
backend on a copy of its transcript, cut just before that message, under a new
session identifier: the original stays intact. Only the Claude backend provides this
position today; elsewhere both conversation options are disabled with the reason
shown, and nothing changes. Restoring the code alone does not touch the
conversation. `u` undoes the last code restore (one level); a conversation restore
cannot be undone yet. `f`, or "fork from here", runs `/fork`.

`/fork [--name <name>] --to <n>` creates the session `<name>-fork` with its own
worktree at the checkpoint, resumes the conversation up to that same message, and
opens a new tab; the original stays intact. A fork refused by the backend (one with
no per-message position) creates nothing.

A single `Esc` always interrupts a turn in progress, as above. Files ignored by git
are not covered by checkpoints.

### What the agent is doing right now

A todo list and a running sub-agent are state, not transcript: written into the
flow they would scroll away from the answer they describe, and rewritten on every
update they would say the same thing a dozen times. So the backend lifts them out
of the stream (`TodoWrite` and `Task` never reach the transcript) and they render
as two panels under it: the list windowed around the task in hand with its
`done/total` count, and one line per live sub-agent with its kind and its age. A
sub-agent leaves the panel when it hands back, and the end of a turn clears both,
keeping an unfinished list. The sub-agents of a turn also hang under the `Task`
that started them as a tree (`├`, `└`, `│`), each with a spinner while it runs, a
`✓` when it hands back, a `✗` when it fails, and `délai dépassé, relancé` when it
timed out and was run again.

→ `plugins/terminal/tui/panels.go`

Picking up a chat session's tab and typing there answers you *there*. A turn says
where it was said, so it is rendered in the window that asked and not posted a
second time in the channel the conversation came from, where nobody asked
anything.

### Themes and look

Four themes ship in the binary: Sombre (the default), Clair, Terminal, which
takes the colours of your own terminal palette, and Contraste élevé. `/theme`,
or `ctrl+x t` from anywhere in the window, opens the theme screen, which takes
the whole window. Moving through the list previews each theme on the spot, and
Entrée keeps the one you chose and the screen stays open, with the line
"enregistré à l’instant" beside the `terminal.toml` block. The current theme
carries a ✓. The same screen
holds the look under RENDU: `g` cycles the glyph set (unicode, nerd, ascii),
`b` the borders (rounded, square, none), `d` the density (normal, compact), `w`
bold and `i` italic. `a` turns the automatic theme on or off, which follows the
terminal's background, and `e` or `ctrl+e` opens `themes/perso.toml` in
`$EDITOR`. Échap closes the screen and puts back the theme it last kept.

The style follows Obel Mono: the accent is #FF3C05 on Sombre and a darker
#C42D00 on Clair, which keeps 5.64:1 contrast on white. Progress bars are 16
`━` cells, filled in the accent, and the spinner turns through braille frames.

The screen also has a police row. The TUI cannot change the font of the
terminal, so the row only recommends Obel Mono and reports whether it is
installed (`fc-list` on Linux, `~/Library/Fonts` on macOS), absent, or unknown.
`f` opens the INSTALLER OBEL MONO block: the install command, which `c` copies,
and the configuration for the terminal it detects, which `y` copies. The TUI
never writes that configuration and never runs the install script. Obel Mono
Masked is the family for kitty and Alacritty. Alacritty's `[font]` section has
no OpenType feature option, so it can only select the family. kitty's
`font_features` is indexed by the font's PostScript name, which this variable
font cannot guarantee, so kitty also gets the family. WezTerm (`harfbuzz_features`)
and foot (`fontfeatures`) select Obel Mono with `ss01`. The Obel Mono page
says of that feature: "Keys show as dots on screen; the file and your clipboard
keep every character." So `ss01` masks API keys on screen only, and the
configuration copied by `y` keeps them intact. Échap closes the block first.

The preview sits beside the list from 100 columns, and under it below that. When
the window is too short for it, the preview is left out so the controls stay on
screen. `NO_COLOR` strips the colours and keeps the rest.

The choice is stored in `terminal.toml`, beside `config.json`:

```toml
[terminal]
theme = "sombre"
auto_theme = false
glyphs = "unicode"
borders = "rounded"
density = "normal"
bold = true
italic = true

[statusline]
source = "builtin"
```

Every writer rewrites only its own keys: the theme picker and `/statusline` share
the file, and the other's settings and your comments survive either one.

The personal theme is `~/.config/herrscher/themes/perso.toml`. The list always
shows it as Mon thème, even before the file exists, and `e` or `ctrl+e` creates
it from a template when it is missing. Keeping Mon thème while its file is absent
is refused, with a message naming the file. Any other TOML file in the `themes`
directory also shows as a theme, named after its id. A theme inherits from a
built-in with `base` and overrides only the colours and render options it names.
The directory is polled, so an edit shows without a restart, and a file that does
not parse is reported on the screen rather than dropped silently.

```toml
name = "Mon thème"
base = "sombre"

[colors]
accent = "#ff8c42"
err = "ansi:1"

[render]
borders = "square"
```

→ `plugins/terminal/tui/theme.go`, `plugins/terminal/tui/theme_catalog.go`

### Approvals

When the agent asks to run something the host does not allow on its own, the
gateway's menu opens an approval box in the session tab, above the composer. It
names the tool and the command (a long command is cut in the middle with `…`),
the project and the worktree, and offers the choices the host sent: `o` allows
once, `s` allows for the session, `t` always allows (the box shows the exact rule
the host will write), and `r` refuses with a reason. Arrows and Enter pick a row
too. Letters only answer while the composer is empty, so typing is never taken
for a choice.

The reason field takes whatever you type; Enter sends the refusal with it, and
an empty reason is a plain refusal. Each answer goes back to the host as the
menu value, so the host decides; the TUI only forwards it.

The box counts down to a refusal. Its deadline is the one the host announces
(an `approval` event), or five minutes when the host gives none; `x` jumps the
countdown to 0:05, and it turns red under a minute. At zero the approval is
refused without a reason, as if nobody answered.

The box is drawn from 60 to 240 columns: below 80 each choice takes two rows.
The tab carries a `pendingApproval` flag for the rest of the window to mark it.

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
a turn completed. A notification does not open a link either: it only signals.

`ctrl+x l` walks the links in the transcript. While one is selected the status line
shows its *resolved target*, since a markdown label can name one destination and
point at another, and `ctrl+x o` opens it: a URL through the desktop handler, a
`file.go:42` reference through `$EDITOR` at that line. That is display, not a
confirmation prompt. The keypress is the consent. With no link selected, `ctrl+x o`
folds or unfolds the latest reasoning block or tool output instead.

### Sessions board and panes

`ctrl+p` (or `/agents`) opens the sessions board: every session, terminal and
other gateways alike, grouped by state (Au travail, Attend une réponse, À relire,
Fini). Each session takes two lines: its state glyph, name, vendor, gateway and
project, then its age, cost, context gauge and last activity. Its running sub-agents
hang under it, one line each with name, vendor and task (three at most, then a
count); a session without a tab in this terminal shows none. Letters filter the
rows, `↑↓` choose, `Enter` opens or switches to the session, and `Esc` clears the
filter before it closes the board. Below 100 columns the board is the list alone.

From 100 columns a Résumé panel sits beside the list. It shows the vendors and
gateways in use, the memory and skills (and how many wait for approval), the
number of live schedules with the next window, and the approval policy (rules and
how many refuse). Each value is read from the host when the board opens and
refreshed while it stays open; a value the host does not answer for reads `n/a`.
A session whose last answer finished while nobody was looking is listed under À
relire until it is opened or stays on screen.

Inside the board, `ctrl+p` switches to the panes: up to four live sessions side
by side, each in a box whose title carries its state glyph, name and vendor, whose
body ends with the last lines of its transcript, and whose foot shows the context
gauge, cost and gateway. The grid is two by two from 120 columns and 30 rows, two
panes side by side from 80 columns, and a single pane moved with `←` `→` below
80. `Tab` changes the focused pane, `ctrl+w` zooms it, and `Enter` sends the
composer text to it. A click on a pane focuses it. When a pane shows an approval,
`o` (once), `s` (session) or `r` (refuse) answers it without leaving the pane.
`ctrl+p` returns to the chat.

### Reading a long session

`ctrl+f` searches the transcript as you type, marks every match, cycles them with
`ctrl+n` and `ctrl+p`, and puts the scroll position back where it was when you
close it.

`ctrl+x k` and `ctrl+x j` jump by turn. `ctrl+x s` opens the session picker, `ctrl+x f`
collapses the history above the current exchange to one line per turn, `alt+c`
folds long code blocks to a summary line, and `ctrl+x c` copies the last one to the
clipboard as raw text.

Each tool call is one line: its family verb in bold, its argument in grey, a
summary, its duration, and on the right `✓`, `✗`, or a spinner while it runs. Its
results hang under it behind `└`, and a click or `alt+e` folds them. Reasoning is a
`▸` block headed `Réflexion · 14s · 1.2k tokens`, in italics, and unfolds to `▾`.
While a turn runs, the transcript ends on a busy line: `En cours…`, the elapsed
time, the tokens sent, the cost, and `Échap interrompre` on the right.

`ctrl+r` searches the prompts you sent, newest first, with the same fuzzy
matching as the command palette: type a fragment, `⏎` recalls the match into the
composer, `esc` closes it.

### Writing while the agent works

The composer stays open while a turn runs. `⏎` queues the message to go out when
the turn ends. `⌥⏎` queues it as a steer: it goes out right after the next tool
call, so the agent can change course without waiting for the whole turn. `esc`
interrupts the turn and keeps the queue, and `⌃⏎` sends the queue now (it
interrupts a running turn first). Queued rows sit above the composer with `✕
retirer` on the right. Remove one by clicking its `✕`, or press `↑` on an empty
composer to focus the queue, then `suppr` on the row; `esc` leaves the focus.

`/queue` lists the queue as a transcript notice.

`ctrl+s` parks the current draft on a stack and clears the composer; `ctrl+s` on
an empty composer brings the last parked draft back. The line above the composer
names the drafts waiting.

`ctrl+x ctrl+e` opens the draft in `$EDITOR` (then `$VISUAL`, then `vi`). The
composer is framed in the accent colour while the editor is open, and the saved
text comes back into it when the editor exits. A terminal that reports `ctrl+⏎`
through the enhanced keyboard protocol sends the queue directly; the chord
`ctrl+x ⏎` does the same everywhere else.

`!command` runs a shell command in the session's directory through the host's
approval policy, the same one that gates the agent's own Bash calls. The output
is shown in the transcript and enters the session as context on the next message,
so the agent sees what you ran. The composer frame turns amber while the line
starts with `!`.

`@` completes a file from the worktree and `/` completes a command. Both are
fuzzy: the characters you type are highlighted in the match, and command rows show
the PR that delivers them on the right.

Tool lines carry a budget of two rows apiece. Unbounded, a compound shell command
becomes a paragraph in a single colour, and the session path it opens with is
identical on every call. So a continuation hangs under the target it belongs to,
an absolute path deep enough to cost a row is said in the two segments that
identify it, and what is left over is counted rather than dropped: `+3 ↵`.
`alt+e` lifts the budget and spells every command out as it was run.

Edits show an inline diff with a line number column for the old file and one for
the new, so an insertion shifts the numbers below it and each row says where it sits
in both. Each hunk opens with an `@@` separator, and additions and deletions carry
their own background, falling back to the basic ANSI pair on a 16-colour terminal,
where a hex green would be approximated away. Under 70 columns the diff keeps a
single number column. A hunk is worth four lines before it folds to
`▶ n lignes de plus (alt+e)`, since a fifty-line diff quoted mid-answer buries the
sentence that follows it; `alt+e`, the same key that spells a command out, unfolds
it. Tables truncate their widest column instead of wrapping into
confetti.

→ [Keys](#keys)

### Reviewing what a turn changed

`ctrl+x d` opens the diff panel over the window. It lists the files the session
touched, with their status (M, A, D), their counts and a reviewed mark, and shows
the selected file as a two-column numbered diff. Scope is the current turn by
default, or the branch against its base when you press `b`. Untracked files count,
and the snapshot runs against a temporary index, so your real index and worktree
are left as they were.

Inside the panel, `space` marks a file reviewed, `i` sends a note to the agent about
the selected file (the message starts with its path), `c` opens the commit message
generated from the changes so you can edit it, `enter` opens the file in `$EDITOR`,
and `p` commits and opens a pull request with `gh`, showing its number. `/review`
opens the same panel in review mode. Git and `gh` calls have a timeout and run in
the session's folder.

### Getting text back out

The window runs on the alternate screen with the mouse captured. That is what
lets the wheel scroll a transcript the terminal keeps no scrollback for, and it
is also why click-drag selection is unavailable exactly where the interesting
text is. So there are two ways out.

`alt+y` puts the last answer on the clipboard, and `/copy [reply|turn|all]` takes
the answer, the whole exchange that produced it, or the tab's transcript. What
was said only, never this window's tool lines, reasoning, costs or notices, which
are noise in a paste.

`ctrl+x m` (or `/mouse`) hands the mouse back to the terminal so its own selection
works as always. The status bar says `mouse → terminal` while it is released,
`pgup` and `pgdn` still scroll, and `ctrl+x m` takes it back.

### Numbers that mean what they say

The context gauge measures the live prompt, read from the counters that arrive
mid-turn. The reply that ends a turn carries the vendor's *billing* totals for
it, which is what the cost line and the budget need, and what the window would
otherwise have shown as an impossible `1252.2k/200k · 100%`. A window opened on a
session that was already running starts from the last measurement the host
recorded, rather than from nothing until the next turn.

The percentage beside the gauge counts down rather than up: it is what remains
before a compaction, which is the question one actually asks of a context gauge. When
automatic compaction is off for a session the gauge reads `pas de compaction auto`
instead.

### Keys

Every key of the terminal is one table: a context, an action id and the chords
that run it. The defaults are Claude Code's own (same context names, action ids
and keys). Where herrscher had another function on a key Claude uses, Claude's
key wins and the herrscher function moved under `ctrl+x` with a mnemonic letter.
`/help` shows the keys of the main window as they are now, and `/keybindings`
opens your overrides file.

#### Main window

Contexts are read in order: Chat first, then Global.

| Key | Action | What it does |
|---|---|---|
| `⏎` | `chat:submit` | send; while a turn runs, queue the message |
| `⌥⏎` | `chat:queueRedirect` | while a turn runs, steer it with the message; when idle, new line. `"alt+enter": null` removes both |
| `⌃J`, `⇧⏎`, `\⏎`, and `⌥⏎` at rest | `chat:newline` | new line |
| `échap` | `chat:cancel` | interrupt the running turn |
| `⇧⇥` | `chat:cycleMode` | cycle the approval mode (Manuel, Édits acceptés, Plan, Auto) |
| `⇥` | `tabs:next` | next session tab |
| `↑` `↓` | `chat:previousHistory`, `chat:nextHistory` | history; `↑` on an empty composer focuses the queue |
| `⌃C` | `app:interrupt` | interrupt a running turn; otherwise quit (the guard below) |
| `⌃D` | `app:exit` | with an empty composer, same guard as `⌃C`; with text in the composer, nothing |
| `⌃R` | `history:search` | search the prompt history |
| `⌃V` | `chat:imagePaste` | paste an image |
| `⌃U` | `chat:removePending` | remove the pending message |
| `⌃S` | `chat:stash` | set the draft aside |
| `⌃L` | `chat:clearInput` | clear the composer |
| `⌃_`, `⌃-`, `⌃⇧-`, `⌃⇧_` | `chat:undo` | undo the last change of the composer (200 steps) |
| `⌃F` | `chat:search` | search the transcript |
| `⌃W` | `app:closeTab` | close the tab |
| `⌃P` | `chat:openBoard` | sessions board |
| `⌃G`, `⌃X ⌃E` | `chat:externalEditor` | edit the draft in `$EDITOR` (then `$VISUAL`) |
| `⌃X ⏎` | `chat:queueSubmit` | send the queue now |
| `⌃X ⌃S`, `alt+F12` | `chat:sendNow` | send now |
| `⌃X ⌃K` | `chat:killAgents` | interrupt every busy session of this window |
| `⌃X C` | `chat:copyCode` | copy the last code block |
| `alt+Y` | `chat:copyReply` | copy the last answer |
| `alt+C` | `chat:toggleCodeFold` | fold long code blocks |
| `alt+E` | `chat:toggleToolExpand` | spell out commands and unfold diffs |
| `⌃O` | `app:toggleTranscript` | the same toggle as `alt+E` |
| `⌃X F` | `chat:toggleTurnFold` | fold the turns |
| `⌃X O` | `chat:openOrFold` | open the selected link, or fold the latest block |
| `⌃X L` | `chat:nextLink` | select the next link |
| `⌃X J` `⌃X K` | `chat:nextTurn`, `chat:previousTurn` | next or previous turn |
| `⌃X M` | `app:toggleMouse` | release the mouse so the terminal can select |
| `⌃X D` | `app:openDiff` | diff panel for the turn or the branch |
| `⌃X S` | `app:openSessions` | session switcher |
| `⌃X T` | `app:openTheme` | theme picker |
| `⌃X V` | `app:toggleSidebar` | toggle the sidebar |
| `?` | `chat:helpToggle` | help, when the composer is empty |
| `n` | `chat:notifyCycle` | cycle the notification, when the composer is empty |
| `/` `@` `!` | not in the table | command, file mention, shell command |

`?` and `n` are typed keys: the table reads them only when the composer is empty,
so they can be rebound like the other keys but still type a character otherwise.

#### Moved keys

These herrscher functions had a key that Claude now uses for its own action.
They moved. The last column names Claude's action on the old key. When that
action is listed under Not yet, the old key shows a notice that names the new key.

| Function | Before | Now | Claude's action on the old key |
|---|---|---|---|
| diff panel | `⌃D` | `⌃X D` | `app:exit` (quit with an empty composer) |
| session switcher | `⌃T` | `⌃X S` | `app:toggleTodos` |
| mouse release | `⌃G` | `⌃X M` | `chat:externalEditor` (opens `$EDITOR`) |
| next link | `⌃L` | `⌃X L` | `chat:clearInput` (clears the composer) |
| open the link, or fold | `⌃O` | `⌃X O` | `app:toggleTranscript` (now toggles the tool detail, see the main table) |
| copy the last code block | `⌃Y` | `⌃X C` | `chat:defaultToNewerModel` |
| sidebar | `⌃B` | `⌃X V` | `task:background` |
| fold the turns | `alt+T` | `⌃X F` | `chat:thinkingToggle` |
| previous or next turn | `alt+↑` `alt+↓` | `⌃X K` `⌃X J` | `app:diffFileListUp` and `app:diffFileListDown` |

#### Not yet

These are names of Claude's reference that herrscher does not run. The first
table holds the fourteen keys that are still bound by default. Pressing one of
them in the chat does nothing but show a short notice that names the action, says
it is not yet available in herrscher, and names the new key when the action is in
Moved keys. The draft in the composer stays as it is.

| Keys | Action | Status |
|---|---|---|
| `⌃X ⌃A`, `⌃X ⇥` | `abovePrompt:toggle`, `abovePrompt:focus` | reserved, does nothing |
| `⌃⇧B` | `app:toggleBrief` | reserved, does nothing |
| `⌃]` | `app:openArtifact` | reserved, does nothing |
| `⌥P` | `chat:modelPicker` | reserved, does nothing |
| `⌥O` | `chat:fastMode` | reserved, does nothing |
| `⌥W` | `chat:workflowKeywordToggle` | reserved, does nothing |
| `⌘K` | `chat:clearScreen` | reserved, does nothing |
| `⌃T` | `app:toggleTodos` | moved to `⌃X S` |
| `⌃Y` | `chat:defaultToNewerModel` | moved to `⌃X C` |
| `⌃B` | `task:background` | moved to `⌃X V` |
| `alt+T` | `chat:thinkingToggle` | moved to `⌃X F` |
| `alt+↑` `alt+↓` | `app:diffFileListUp`, `app:diffFileListDown` | moved to `⌃X K` `⌃X J` |

The table accepts the other names of Claude's reference without a message,
because herrscher does not run them yet. Each is listed by its action name,
grouped by context prefix, and none is bound or executed here:

- `abovePrompt:highlightNext`, `abovePrompt:highlightPrevious`, `abovePrompt:leave`, `abovePrompt:next`, `abovePrompt:press`, `abovePrompt:previous`
- `agents:find`, `agents:nextGroup`, `agents:previousGroup`, `agents:rename`, `agents:switchView`, `agents:togglePin`
- `app:redraw`, `app:toggleDiffNoiseFilter`, `app:toggleDiffPreSession`, `app:toggleReplTab`, `app:toggleTerminal`
- `attachments:exit`, `attachments:next`, `attachments:previous`, `attachments:remove`
- `autocomplete:next`, `autocomplete:previous`
- `confirm:cycleMode`, `confirm:next`, `confirm:nextField`, `confirm:previous`, `confirm:previousField`, `confirm:toggle`
- `diff:back`, `diff:nextSource`, `diff:previousSource`
- `effortSlider:decreaseEffort`, `effortSlider:increaseEffort`, `effortSlider:thisSessionOnly`, `effortSlider:toggleUltracode`
- `footer:clearSelection`, `footer:close`, `footer:dismiss`, `footer:down`, `footer:next`, `footer:openSelected`, `footer:previous`, `footer:up`
- `help:dismiss`
- `historySearch:cycleScope`, `historySearch:execute`
- `modelPicker:decreaseEffort`, `modelPicker:increaseEffort`, `modelPicker:thisSessionOnly`
- `pane:bottom`, `pane:grow`, `pane:pageDown`, `pane:pageUp`, `pane:scrollDown`, `pane:scrollUp`, `pane:shrink`, `pane:top`
- `permission:toggleDebug`
- `plugin:cycleMarketplace`, `plugin:favorite`, `plugin:install`, `plugin:toggle`
- `scroll:bottom`, `scroll:fullPageDown`, `scroll:fullPageUp`, `scroll:halfPageDown`, `scroll:halfPageUp`, `scroll:lineDown`, `scroll:lineUp`, `scroll:top`
- `selection:clear`, `selection:copy`, `selection:extendDown`, `selection:extendLeft`, `selection:extendLineEnd`, `selection:extendLineStart`, `selection:extendRight`, `selection:extendUp`
- `settings:fixUsage`, `settings:periodDay`, `settings:periodWeek`, `settings:retry`, `settings:search`, `settings:sortByTokens`
- `tabs:previous`
- `theme:toggleSyntaxHighlighting`
- `transcript:exit`, `transcript:toggleShowAll`
- `voice:pushToTalk`

Of Claude's 148 reference names, 44 run here, the fourteen in the first table are
bound by default and do nothing yet, and the 90 bullets make up the rest. The
names are kept in `plugins/terminal/tui/keys_claude_reference.go`, checked against
a frozen copy of the reference in `testdata`, and a test loads a Claude file for
each of them and requires no warning.

#### Quitting: `⌃C` and `⌃D`

With nothing running, `⌃C` and `⌃D` (with an empty composer) quit at once. When
a turn runs in the active session, the first press interrupts it and a second
press quits. When another session is busy, the first press warns and names how
many sessions will stop, and a second press within 3 seconds quits. Inside a
dialog (a commit message or its editor, an approval reason), `⌃C` closes it
as `⎋` does, keeps the text, and neither quits nor interrupts the turn. In the
diff note `⌃C` does nothing, because closing the note would drop what is typed
there: `⎋` closes it. Quitting a served daemon
stops the sessions it runs, as described above.

#### Your keys: `~/.config/herrscher/keybindings.json`

The file has Claude Code's exact format, so a file copied from
`~/.claude/keybindings.json` works as it is:

```json
{
  "$schema": "https://www.schemastore.org/claude-code-keybindings.json",
  "$docs": "https://code.claude.com/docs/en/keybindings",
  "bindings": [
    { "context": "Chat", "bindings": { "ctrl+e": "chat:externalEditor", "ctrl+g": null } }
  ]
}
```

- Your bindings are added after the defaults. A later binding on the same chord
  in the same context replaces the default, and `null` removes a default.
- A chord is one or more keys separated by a space (`ctrl+x ctrl+e`). Modifiers
  are `ctrl`, `alt` (also `meta`, `opt`), `shift` and `cmd` (also `super`, `win`).
- Not remappable: `ctrl+c`, `ctrl+m`, `ctrl+[`, `ctrl+i`, `ctrl+h` (they are the
  same bytes as other keys in a terminal), and `ctrl+d` as an exit key. `ctrl+z`
  and `ctrl+\` are reserved by the terminal.
- An invalid entry is skipped and reported in the banner: an unknown context,
  an unknown modifier, an unknown action, a duplicate in one context, a key that
  cannot be used, or a value that is neither an action nor `null`. The rest of
  the file still applies. A file that is not valid JSON is skipped as a whole,
  and reported.

#### Claude's file and `[keys]`

By default herrscher also reads `~/.claude/keybindings.json` before its own
file, so your Claude personalisations apply here too. Claude actions that
herrscher does not run are accepted without a message, while an action name
that neither product knows is reported. The `[keys]` section of
`terminal.toml` controls this:

```toml
[keys]
import_claude = false
chord_timeout = 3
```

`chord_timeout` is the delay in seconds between two keys of a chord (default 3).

#### `/keybindings`

`/keybindings` opens the herrscher file in `$EDITOR`. It creates the file with
`$schema`, `$docs` and `bindings: []` when it does not exist, and reloads the
table when the editor closes, flashing the problems if there are any.

#### Overlays and footers

The overlays (diff, sessions, search, rewind, theme, status bar, plugins) still
read their own keys, and their footers and the composer placeholder still show
the built-in keys. A changed key shows in `/help` and in the chat bindings, not
yet in those footers.

→ `plugins/terminal/tui/keys.go`, `plugins/terminal/tui/keys_defaults.go`,
`plugins/terminal/tui/keys_file.go`, `plugins/terminal/tui/keys_runtime.go`

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
| Compactor | `Compactor` | [herrscher-jev-compactor] |

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
[herrscher-jev-compactor]: https://github.com/Herrscherd/herrscher-jev-compactor
