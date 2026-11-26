# What a session gives you

## Durable sessions

Each session is a channel, an agent and an isolated git worktree, supervised and
restarted automatically. The conversation resumes from the backend's own resume
token, so a restart is not a new conversation. If the backend refuses that token
before it answers, the same turn runs on a fresh backend session instead of
failing: the conversation goes on, without the backend's earlier history.

A session can be created explicitly or adopt an existing one with `--channel_id`.
On Discord it opens the first time its owner pings the bot in a channel, and on
Telegram with the first message of a private chat, a group or a forum topic. A
new session lands in the daemon's home unless `--under` names another category
or forum, so work opened from one server does not surface in another.

A session that adopts a conversation of its one gateway (`--gateways telegram`)
is announced in that conversation: its banner and its account notice go through
that gateway, never through the home's. A gateway that manages no channels gets
the banner as a plain post, and a gateway that is not running gets nothing: the
session starts, its banner is skipped with a warning in the daemon's log.


## Memory

An Obsidian vault, provisioned for you, scoped shared-per-project and
private-per-agent, and recalled before every turn.


## Learning

Opt-in consolidation of a session's work into scoped nodes, with aging, semantic
merge, cross-agent promotion, and archiving you can undo.


## Skills the agent writes for itself

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


## Delegation across vendors

An agent ends a turn with a `⟢` trailer to delegate, fan out, route, merge or
hand off. Workers inherit their agent's vendor, so one run can mix Claude, Codex,
Cursor, Grok and Antigravity.

## Choosing a session's vendor

Without `--model`, a session's vendor is the first of these that is set:
`--vendor`, then the vendor whose CLI the `--cmd` binary is (`claude`, `codex`,
`cursor-agent`, `grok` or `agy`), then `HERRSCHER_BACKEND`. A `--cmd` left unset is the
host's default command, and a vendor whose CLI is not that command runs its own
default instead, so `--vendor grok` runs `grok` without a `--cmd`. With `--model`, the model names its
vendor: `--vendor` or `HERRSCHER_BACKEND`, when set, must agree with it, and
without either the model's vendor is used.


## Agents that start their own turns

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


## Files a backend sends back

A backend that writes files during a turn, such as a chart or an image, declares
`FileProducer`. For each of its turns the bridge makes a private directory
(0700) under the daemon state directory, in `outbox/<session>/`, and hands its
path to the backend as `Prompt.Outbox`. After the reply, the bridge takes the
files the backend lists and vets them: each must be a regular file inside that
directory, and no larger than the limit. At most 10 files a turn are sent, and a
refused file is named in the reply with a notice, its content never logged.

The limit is 25 MB by default. `outboundFileMaxMB` in `config.json` sets another
size in megabytes, up to 1024. The daemon delivers the reply text first, as a
normal reply. The valid files then go through the gateway's `SendFiles` when the
gateway implements it, as a separate message without a reply target, because an
event carries no message ID to reply to. A gateway that does not implement
`SendFiles` gets a notice that names the files, without their paths. The files
go in batches of at most 10 files and 9 MiB, the limits of a Discord message.
The Discord gateway also refuses any single file over 8 MiB and names it in its
own message, so a limit above 8 MB only helps other gateways. A batch that fails
gets a notice that names only its own files, and the batches before and after it
are still sent. A batch whose send times out is named as not sent even when the
gateway delivered it late. When the turn is cancelled, the batches not yet sent
are named in a single notice. The turn directory is removed once the reply
is delivered, or once the turn ends without files. A directory a crash leaves
behind is purged when it is older than 24 hours: at the daemon start for every
session, and at the start of each turn for the outbox of that turn's session
only, so a turn still running in another session is never removed. When the
reply goes to another conversation than the one that sent the turn, its files
are not sent, and the reply names them in a notice, refused files included. A
backend without `FileProducer` gets no outbox, and its replies are unchanged.

