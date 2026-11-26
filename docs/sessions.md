# What a session gives you

## Durable sessions

Each session is a channel, an agent and an isolated git worktree, supervised and
restarted automatically. The conversation resumes from the backend's own resume
token, so a restart is not a new conversation. If the backend refuses that token
before it answers, the same turn runs on a fresh backend session instead of
failing: the conversation goes on, without the backend's earlier history.

A session can be created explicitly or adopt an existing one with `--channel_id`.
On Discord it opens the first time its owner pings the bot in a channel. A new
session lands in the daemon's home unless `--under` names another category or
forum, so work opened from one server does not surface in another.

Renaming a session keeps its worktree `.herrscher-sessions/<id>/<old name>` and
its branch `session/<id>/<old name>`: the agent runs in that folder and the
branch may carry a pull request. Closing it, spawning or merging from it, and
reporting its branch all read the branch checked out in the recorded worktree,
never one derived from the current name. Creating a new session under the old
name is refused while the renamed session still holds that folder (or that
scratch directory): pick another name, or close the renamed session first.

Checkpoints (see [rewinding](terminal-ui.md#rewinding)) are git refs
`refs/herrscher/checkpoints/<incarnation>/<n>`, with the undo point at
`refs/herrscher/undo/<incarnation>`. The incarnation is the random identity the
state gives each session object: a rename keeps it, while closing a session and
creating another under the same name gives the new one a new incarnation. So a
renamed session lists and rewinds the checkpoints it took under its earlier
name, and a session that takes a name freed by a rename or a close sees none of
the checkpoints of the session that had it. The session id (`s_<name>`) is not
used: it is handed out again to a new session once the old one is closed. The
refs live in the repository of the worktree and are shared by all its
worktrees. Closing a session leaves its refs where they are, under an
incarnation no other session gets. Only local sessions take checkpoints: a
session on another [host](remote.md), or in a scratch directory, has none.


## The state file and its migration

`state.json` carries a `format` number. This build writes format 1. A file
without the field is format 0, written by a build from before the field existed.

`herrscher serve` migrates a format 0 file once, at boot, before it supervises
any session. It holds the state lock while it does, so no other daemon and no
session of this state runs during the migration. Before it moves anything, it
writes a copy of the old file, byte for byte as it was read, next to it as
`state.json.before-format-1.bak` (mode 0600, written to a temporary file and then
renamed). A copy left by an earlier migration is never overwritten: the next one
is `state.json.before-format-1.2.bak`, then `.3.bak`, and the start of the
migration names the exact file. If that copy cannot be written, the migration
refuses before it touches a worktree or a branch. Then it moves what it has to,
and writes the new file atomically. Each change is announced on the output of
`serve` and in the daemon log, in the operator language:

- **Instance id.** A state without an instance id gets one. It comes from
  `HERRSCHER_INSTANCE_ID`, else from `HERRSCHER_OWNER_ID`, else it is generated
  once (`h` and eight random characters) and written into the state. A daemon
  never runs without an id any more: the id-less layout, its bare branch names
  and its bare socket names are gone.
- **Sessions of the id-less layout.** A session whose worktree is
  `<repo>/.herrscher-sessions/<folder>` moves to
  `<repo>/.herrscher-sessions/<id>/<name>` with `git worktree move`, and its
  branch `session/<folder>` is renamed `session/<id>/<name>`. `<folder>` is the
  name the session had when its worktree was made, so a session renamed since
  then lands under its current name. A scratch directory
  `.herrscher-scratch/<folder>` moves to `.herrscher-scratch/<id>/<name>`. The
  session's stored worktree and directory follow. Uncommitted work moves with the
  directory, since the move is a rename. Only what actually moved is announced: a
  directory already at its new place, or found nowhere, is not. Sockets are not
  moved: their paths are derived from the id each time the daemon starts, so the
  new ones are simply used, and the bridges the daemon starts after the
  migration dial them.
- **Conversations.** A moved session that holds a resume token is announced as
  starting a new conversation on its next turn. A backend that files its
  conversations by working directory (Claude Code keeps them under
  `~/.claude/projects/<encoded folder>/`) does not find the old one from the new
  folder, refuses the token, and starts afresh, as it does for any resume token
  it refuses (see above). The old conversation file stays where it was.
- **Gateway binding.** A session with a channel and no gateway binding is bound
  to the channel gateways this daemon builds (every built gateway but the
  terminal), which is the binding it was already using implicitly.
- **Checkpoint refs.** The checkpoints and the undo point a local session
  filed under its current name (`refs/herrscher/checkpoints/<name>/`,
  `refs/herrscher/undo/<name>`) move under its incarnation, in one git
  transaction per session (`git update-ref --stdin`), before any worktree
  moves. A ref already at its new place with the same commit counts as moved,
  so a migration stopped after the move finishes it, and a second run moves
  nothing, and the announce says how many checkpoints moved, whether the undo
  point moved, and how many refs were already in place and only lost their old
  name. Refs a session filed under a name it had before a rename are left
  where they are, as are those of closed sessions.
- **Folders in no git repository.** A local session whose folder is gone, or
  belongs to no git repository (git says `not a git repository`), holds no
  checkpoint ref and has no worktree git could move: the migration leaves it
  where it is, its stored worktree unchanged, and announces nothing for it.
- **Old `repo` field.** It becomes the workspace when neither the state nor
  `config.json` sets one, and is dropped otherwise, since a workspace already
  applied in its place.

The migration only touches local files and local git refs. It sends no message,
to a channel or to anyone, and renames no channel: an existing session is
addressed by its channel id, so its title, given at creation, stays as it is.
New sessions get the `<id>__<name>` title.

It refuses, writes nothing, and says what to do instead of guessing, when:

- the target directory of a move already exists, or the target branch already
  exists;
- the branch to rename tracks a remote branch, since renaming it would cut it off
  from its pull request (merge or close the pull request, run
  `git branch --unset-upstream session/<name>`, then start `serve` again);
- the branch to rename was pushed without tracking, so a remote-tracking ref
  `<remote>/session/<name>` exists, or the session records a pull request and
  `origin` still holds the branch: the pull request would stop receiving pushes.
  Once it is merged or closed, delete the branch on the remote (`git push
  <remote> --delete session/<name>`, or `git branch -dr <remote>/session/<name>`
  when it is already gone there) and start `serve` again;
- the session records a pull request and `origin` cannot be asked whether it
  still holds the branch, including when the forge token of the connection
  cannot be refreshed. Only `origin`, the remote pull requests push to, is
  asked, with that token. Git runs with its terminal prompt and askpass program
  turned off, and ssh in batch mode with a ten second connect timeout, unless
  `GIT_SSH_COMMAND`, `GIT_SSH` or `core.sshCommand` already choose the ssh
  command, which then decides whether it prompts. Getting the token and asking
  `origin` share one limit of a minute. The refusal names the session and
  prints the commands that move it by hand (`mkdir -p`, `git worktree move`,
  `git branch -m`), which need no network: run them once the pull request is
  merged or closed, or start `serve` again once `origin` answers;
- the worktree is checked out on another branch than `session/<name>`;
- a session, or the folder of its worktree, is named like the instance id: the
  instance layout would nest every other session inside it (set
  `HERRSCHER_INSTANCE_ID` to another id);
- a session has a channel and no binding, and this `serve` builds no channel
  gateway to bind it to;
- a checkpoint ref a session's checkpoints would move to already holds another
  commit. The refusal names the ref and both commits; look at it with
  `git log -1 <ref>`, delete it with `git update-ref -d <ref>` if it is not
  wanted, and start `serve` again.
- git cannot read the checkpoint refs of a session's folder for another reason
  than the folder being in no repository: a worktree whose repository moved, a
  folder git calls of dubious ownership, a folder it may not read. The refusal
  names the session, the folder and git's error; make
  `git -C <folder> for-each-ref refs/herrscher/` work (fix the rights, add the
  folder to `safe.directory`, or run `git worktree repair <folder>` from its
  repository), and start `serve` again.

A session that runs on another [host](remote.md) is not refused: the migration
cannot reach that machine, so it points the state at the new place and prints,
for that session, the commands to run on the host (`mkdir -p`, `git worktree
move`, `git branch -m`, or a plain `mv` for a scratch directory). Until they are
run, the session fails to start on the host and the daemon keeps retrying.

A move that fails halfway is put back, and the next start picks the migration up
where it stopped: a directory or branch already at its new place counts as done.
Run twice, the migration changes nothing the second time.

A file of a newer format than the build is refused at load, by `serve` and by
every command, with the file and both formats named. Commands run outside the
daemon refuse a format 0 file and ask to restart the daemon, since only the
daemon has the gateways and the lock the migration needs. That is also what an
upgrade with the old daemon still running looks like: the new client finds no
daemon on the socket of the new instance id, falls back to running the command
itself, and meets the old file. `herrscher serve` cannot start while the old
daemon holds the lock, so stop the old daemon first, then run `herrscher serve`.


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
hand off. Workers inherit their agent's vendor, so one run can mix Claude, Codex
and Cursor.

## Choosing a session's vendor

Without `--model`, a session's vendor is the first of these that is set:
`--vendor`, then the vendor whose CLI the `--cmd` binary is (`claude`, `codex`
or `cursor`), then `HERRSCHER_BACKEND`. With `--model`, the model names its
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

