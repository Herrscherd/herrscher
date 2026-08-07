---
name: move-the-conversation
description: Use when the operator asks to continue the discussion somewhere else — "open this in the TUI", "ouvre-moi la conv ailleurs", "make me a session for that" — or asks you to open, close, or list Herrscher sessions. Covers what a session already is across channels, and driving the herrscher CLI from your own shell.
---

# Continuing the conversation somewhere else

## Your channels are already one conversation

A Herrscher session is not bound to the channel you happen to be reading. One
daemon runs every gateway it was configured with — a chat gateway, the terminal
UI — and a session binds to a *set* of them. A message arriving from any bound
channel enters the same turn loop and lands in the same history.

So there is nothing to migrate. "Continue this in the terminal" is not a move
and needs no handoff brief: open the TUI on the running daemon and the session is
already there, mid-conversation, with everything it knows. Telling the operator
you will "transfer the context" between two of your own channels invents work
that does not exist, and re-pasting a summary into a session that already
remembers is noise.

What *is* a real boundary is another agent process: a workspace opened in a
separate harness, or a new Herrscher session. Those start with no memory of this
conversation. That is the only case where a brief is owed — see below.

## The one rule about when

Do it when asked. Never open a session, workspace, or channel on your own
initiative — these cost money and clutter the operator's workspace, and an
unrequested one is noise they have to clean up. Asking "shall we continue?" is a
question about the work; it is not permission to spawn anything.

If a request is ambiguous about *where* (which project, which branch), ask that
one question rather than guessing. Guessing the project wrongly puts an agent to
work on the wrong repo.

## Read the request before you reach for a tool

Two different asks sound alike and take different tools:

- **"open a Herrscher session on X"** → `herrscher session create`. This mints a
  new conversation on the daemon's gateways and starts an agent in it.
- **"open it in superset / in the other harness"** → that is a *different* tool
  (see below), not `session create`. Running `session create` for it puts an
  agent somewhere the operator did not ask for and leaves them chasing it.

When the request names *where* — a server, a project, a channel — that place is
part of the instruction, not decoration. Carry it into the command (`--under`,
`--channel_id`, `--project`) instead of dropping it and letting the default win.

## Driving Herrscher from your shell

The `herrscher` CLI talks to the live daemon, so it reads and changes real state.
These are the session verbs:

```bash
herrscher session list                  # what is running right now
herrscher session create --name <slug> --project <dir> [--model <id>] [--vendor <backend>]
herrscher session close --name <slug>   # --force discards uncommitted work
herrscher session archive --name <slug> # keeps it resumable
herrscher models list                   # what --model accepts
```

They are not the whole surface. The gateway plugins a daemon runs contribute
verbs of their own, namespaced under the plugin's kind, and they exist only in
that daemon — so which ones you have depends on how this host was composed. Ask
it rather than guessing:

```bash
herrscher commands                      # every verb the running daemon accepts
herrscher commands --json               # same, with each verb's params
```

Any verb it lists can be invoked directly (`herrscher <kind> <verb> …`); the CLI
relays it to the daemon. With no daemon running, such a verb is simply unknown.

Never create throwaway sessions to discover the syntax: each one is a real
channel and a real agent. `herrscher <group> <verb> --help` costs nothing.

If the bare name does not resolve, the daemon's own environment is narrower than
a login shell's; fall back to `"$(go env GOPATH)"/bin/herrscher` rather than
reporting the CLI as missing.

### Where the new conversation lands

By default `session create` mints its channel under the daemon's *home* — one
container, configured once, usually in whatever server the operator set up first.
So a session opened while reading a channel in another server surfaces somewhere
else entirely, which reads as the agent wandering off. Two flags say "here"
instead:

```bash
herrscher session create --name <slug> --under <category-or-forum-id>  # create it there
herrscher session create --name <slug> --channel_id <channel-id>       # adopt this conversation
```

`--under` needs a container (a category or a forum); a plain channel is a
conversation, so adopt it with `--channel_id`. Pass one or the other, never both.
When you are already reading a channel and the operator says "open it here", that
channel's container is the answer — not the home.

`--force` on close **discards uncommitted work in the worktree**. Never pass it
to silence an error; if a close is refused because the tree is dirty, say so and
let the operator decide.

Do not pass `--state` to "open" a session in a new window: that starts a second
daemon with its own world, which is the opposite of what was asked. A bare
`herrscher` on a TTY already attaches to the running daemon and shows its
sessions as tabs.

## Opening work in another harness

Some machines run a separate workspace tool — `superset` is the one you are most
likely to meet — that can create a worktree and start an agent in it. When one is
present, it is the right way to put a *different* agent on a *different* branch —
not a way to relocate this conversation. When the operator names it ("ouvre ça
sur superset"), that tool is the instruction: `herrscher session create` is not a
substitute for it and does not land in the same place.

Before reaching for it, check it is actually there (`command -v <tool>`) and say
plainly that it is not when it is not. A desktop app's CLI is often a shim into a
mount that only exists while the app runs, so a "command not found" usually means
the app is down rather than that you got the invocation wrong — report that and
stop instead of hunting for the real binary, whose path will be stale by the next
launch. If it needs credentials you do not have, say so: an interactive login is
the operator's to run, never yours to attempt, because you will hang on it.

Prefer the form that starts no agent when you only need the worktree — it costs
nothing. Check the existing workspaces before creating one: a second workspace on
the same branch splits the work in two. And when a delete leaves the git branch
behind, say so rather than letting stale branches accumulate silently.

### The brief is the whole point

Whatever prompt you hand a new agent is all it will ever know about this
conversation. What you leave out, the operator retypes — which is the exact
friction they were trying to escape.

Write a brief that lets the other agent start working, not one that makes it ask
why it is here:

- **What is being built**, in the operator's own framing — a restatement drifts.
- **What was already decided**, and what is still open, especially the questions
  the operator has not answered yet, so they are not asked again.
- **What was found in the code**: files, functions, the constraints that matter.
- **The immediate next step.**

## Afterwards

Say where it went and how to reach it — the session or workspace name, and its id
if the tool gave you one. A move the operator cannot follow is a move that lost
them.
