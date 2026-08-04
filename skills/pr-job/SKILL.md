---
name: pr-job
description: Use when a task arrives from chat and must end as a pull request — fix in the session worktree, review it against every axis, then open the PR.
---

# Finishing a chat-triggered job as a PR

You are working in this session's own git worktree. The job ends with a pull
request, not with a message saying the work is done.

## 1. Understand before touching anything

Read the request and every screenshot attached to it. The screenshots are the
report: reproduce what they show before you decide what is broken. If the request
is ambiguous in a way that changes what you would build, end your turn with a
question and concrete options — the gateway renders them as a menu and your next
turn receives the answer. Ask once, with real options; do not ask what you can
determine by reading the code.

## 2. Fix

Make the smallest change that actually fixes the reported problem. Follow the
surrounding code's patterns, naming and comment density.

## 3. Review your own diff, on every axis

Go through these in order and fix what you find. Report only what you verified —
a finding you cannot demonstrate is a false positive, and false positives cost
more than silence.

- **CI compliance** — run the project's own build, tests, vet and lint. Paste the
  real output. Never claim green without it.
- **Architecture** — does the change respect the project's boundaries? A layer
  that suddenly knows about another layer's concrete types is a defect, not a
  shortcut.
- **Performance** — new allocations in hot paths, N+1 calls, unbounded goroutines,
  work done per-item that could be done once.
- **Code quality** — dead code, duplicated logic, names that do not say what the
  thing is, error paths that swallow the cause.
- **Security** — untrusted input reaching the filesystem, a shell, or a network
  call; secrets in logs; permissions on files you create.
- **Bug review** — read the diff as an adversary. What input makes this wrong?
  What happens on the empty, the concurrent, and the restart case?
- **Useless comments** — delete comments that restate the code. Keep the ones
  that explain *why*.
- **Docs** — update the README and any doc the change contradicts, so the docs
  describe the code as it is now.

## 4. Open the PR

Commit with a conventional-commit message, push the session branch, and open the
PR with `gh pr create`, whose body states what changed, why, and how you verified
it. Post the PR URL as your reply so it lands in the channel.
