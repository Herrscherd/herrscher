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

## Driving Herrscher from your shell

The `herrscher` CLI talks to the live daemon, so it reads and changes real state:

```bash
herrscher session list                  # what is running right now
herrscher session create --name <slug> --project <dir> [--model <id>] [--vendor <backend>]
herrscher session close --name <slug>   # --force discards uncommitted work
herrscher session archive --name <slug> # keeps it resumable
herrscher models list                   # what --model accepts
```

If the bare name does not resolve, the daemon's own environment is narrower than
a login shell's; fall back to `"$(go env GOPATH)"/bin/herrscher` rather than
reporting the CLI as missing.

`session create` also mints the conversation on whichever gateways the daemon is
bound to, so it is how you open a fresh channel for a piece of work.

`--force` on close **discards uncommitted work in the worktree**. Never pass it
to silence an error; if a close is refused because the tree is dirty, say so and
let the operator decide.

Do not start a second daemon to "open" a session. A bare `herrscher` on a TTY
serves — it does not attach — so a second one is a second world with its own
state, which is the opposite of what was asked.

## Opening work in another harness

Some machines run a separate workspace tool that can create a worktree and start
an agent in it. When one is present, it is the right way to put a *different*
agent on a *different* branch — not a way to relocate this conversation.

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
