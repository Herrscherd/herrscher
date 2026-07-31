# G6 — Bounded USER.md — design

**Date:** 2026-07-30
**Status:** Design (approved)
**Slice:** G6 of the Hermes-parity roadmap
(`docs/superpowers/specs/2026-07-30-memory-learning-hermes-parity-design.md`)
**Repos:** `herrscher-contracts` (budget helper), host `core/internal/agent`
(home + materialize + config wiring).

## Goal

Add a bounded `USER.md` — a durable profile of the **human** the agent serves
(identity, preferences) — to the agent home, separate from `SOUL.md` (the
agent's own persona) and from project memory facts. Materialize it into the
session worktree so the agent actually reads it, and bound its size with the
G1 budget policy so it stays terse instead of leaking.

This is the Hermes `USER.md` analogue: a small, injected-at-session-start user
profile, distinct from `MEMORY.md` (facts) and the skills layer.

## Baseline (verified in code, 2026-07-30)

- `core/internal/agent/agent.go` — `Agent.Materialize(worktree)` copies home
  files into the worktree: `SOUL.md` → both `.claude/CLAUDE.md` and `AGENTS.md`;
  `mcp.json` → `.mcp.json` (+ `.codex/config.toml`); `settings.json` →
  `.claude/settings.json`. `worktreeToken` (`{{WORKTREE}}`) is substituted.
- `core/internal/agent/store.go` — `Store` owns `<root>/<name>/`. `Create` seeds
  `SOUL.md`/`mcp.json`/`settings.json` (+ optional `TAGS`/`backend`/`cmd`).
  `SetSoul(name, soul)` overwrites `SOUL.md` for an existing home; it never
  creates one. `Get`/`List` read home metadata.
- G1 shipped: `contracts.BudgetError{Key, Runes, Limit}` (shipped in contracts
  v0.2.9 alongside G3); obsidian enforces it inline in `recordUnlocked` (rune
  count, `limit<=0` disables). The rune-count + typed-error policy currently
  lives only in obsidian. Current released floors: contracts **v0.2.9**,
  obsidian **v0.2.7**, orchestrator **v0.1.12**.

## Design

### 1. Home model (`core/internal/agent`)

- New home file constant `userFile = "USER.md"`. It holds the user profile.
  Its absence is valid — an agent with no user profile yet is normal.
- `CreateSpec` gains an optional `User string`. When non-empty, `Create` seeds
  `<home>/USER.md`. Empty → no file written (no default profile).
- New `Store.SetUser(name, text string) error`, mirroring `SetSoul`: requires an
  existing home (absent agent → error), overwrites `<home>/USER.md`. It enforces
  the budget **before** writing (see §2).
- `Store` gains a `userBudget int` field. `NewStore` initializes it to
  `defaultUserBudget = 1500` (runes). New `Store.SetUserBudget(runes int)`
  overrides it; `<= 0` disables the check. This mirrors obsidian's
  `SetNodeBudget`.
- No new field on `Agent` — `USER.md`, like `SOUL.md`, is read at materialize
  time, not cached on the struct.

### 2. Enforcement (`herrscher-contracts` — already shipped in v0.2.9)

**Update 2026-07-30:** `contracts.EnforceBudget` already exists in the released
v0.2.9 (it landed alongside G3, verified in `v0.2.9:budget.go`). G6 therefore
needs **no contracts change and no new tag** — the host already depends on
v0.2.9 and can call it directly. This slice is host-only.

The canonical helper (for reference):

```go
// EnforceBudget returns a *BudgetError when body exceeds limit runes.
// A limit <= 0 disables the check and returns nil. key labels the rejected item
// in the error. Rune count (not bytes) is authoritative.
func EnforceBudget(key, body string, limit int) error {
    if limit <= 0 {
        return nil
    }
    if r := utf8.RuneCountInString(body); r > limit {
        return &BudgetError{Key: key, Runes: r, Limit: limit}
    }
    return nil
}
```

`Store.SetUser` calls `contracts.EnforceBudget(name, text, s.userBudget)` and
returns its error (a `*contracts.BudgetError`, matchable with `errors.As`)
before touching disk. This also gives the deferred G1 Minor ("negative budget
silently disables") a single, documented home. obsidian's adoption of the
helper is **out of scope** here (no obsidian re-tag).

### 3. Materialization (`Agent.Materialize`)

When `<home>/USER.md` exists (read it once; `os.IsNotExist` → treat as absent):

- Write `<worktree>/.claude/USER.md` from it, with `worktreeToken` substituted —
  the standalone artifact.
- `CLAUDE.md` = `SOUL.md` content + a trailing import line so Claude Code
  auto-loads the profile as a separate file. The import is `@USER.md` (bare
  sibling), not `@.claude/USER.md`: Claude Code resolves relative imports
  against the importing file's own directory, and CLAUDE.md already lives in
  `.claude/`, so a `.claude/` prefix would point at the nonexistent
  `.claude/.claude/USER.md`.

  ```
  <SOUL.md contents>

  @USER.md
  ```

- `AGENTS.md` = `SOUL.md` content + the `USER.md` content inlined under a
  `# User` heading (Codex reads `AGENTS.md` and has no `@import` mechanism):

  ```
  <SOUL.md contents>

  # User

  <USER.md contents>
  ```

When `<home>/USER.md` is absent, `CLAUDE.md` and `AGENTS.md` are exactly today's
output (`SOUL.md` verbatim) — a regression test locks this.

`worktreeToken` substitution applies to the USER-derived output the same way it
does to the existing copies.

### 4. Config wiring (host)

Where the host constructs the agent `Store`, read `AGENT_USER_BUDGET` (env, as
the obsidian plugin reads `OBSIDIAN_NODE_BUDGET`); when set, call
`SetUserBudget`. Absent/unparseable → keep the `1500` default. Exact wiring
point identified in the plan.

## Testing

**contracts (`EnforceBudget`):** over budget → `*BudgetError` with the right
`Runes`/`Limit`/`Key`; under → nil; `limit <= 0` → nil (disabled); a
100-rune/200-byte string (`strings.Repeat("é", 100)`) against limit 50 proves
runes, not bytes, are counted.

**agent (`SetUser`):** over budget → `*BudgetError` (assert `Runes`/`Limit` via
`errors.As`); under → file written with exact content; budget 0 → over-size
write allowed; `SetUser` on a missing agent → error and no file created.

**agent (`Materialize`):** with `USER.md` present → `.claude/USER.md` written,
`CLAUDE.md` contains the `@USER.md` import after the soul, `AGENTS.md`
contains the inlined `# User` section; `worktreeToken` substituted. Without
`USER.md` → `CLAUDE.md` and `AGENTS.md` equal the soul verbatim (regression).

## Release footprint

- `herrscher-contracts` — **no change**. `EnforceBudget` already ships in the
  released v0.2.9 (the host already depends on it). No contracts tag, no
  `go.mod` bump, no `go.work` overlay.
- `core/internal/agent` is part of the host module → shipped with the host; no
  obsidian tag this slice (obsidian keeps v0.2.7). **G6 is host-only.**

## Out of scope (YAGNI)

- The Learner writing `USER.md` automatically (future slice; `SetUser` is the
  seam it will call).
- Migrating obsidian's inline check to `contracts.EnforceBudget` (later).
- Any merge / staleness / archival behavior for `USER.md`.

## Invariants (from the umbrella roadmap)

1. **Ports only, policy not engine** — the budget is policy expressed through
   `contracts`; no new storage engine.
2. **Learning never breaks a turn** — `SetUser` is off the turn path.
3. **Reversible over destructive** — `SetUser` overwrites; nothing is
   hard-deleted.
