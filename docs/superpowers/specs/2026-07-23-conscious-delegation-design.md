# Conscious cross-model Delegation — design

**Date:** 2026-07-23
**Status:** approved (design)
**Goal:** Make the running model *aware* it can hand a full mission to another
backend (Codex) and get the result back asynchronously — by framing the
delegation machinery that **already exists** (`coordinator`, `⟢` trailer
markers, async `Report`), not by building new coordination.

## Problem

Herrscher already has a complete, model-driven, asynchronous delegation stack:

- `coordinator.Delegate` spawns a worker child session off the lead's committed
  tip, in an isolated worktree, recording the lead as parent (`coordinator.go`).
- The worker runs on whatever backend its **agent** declares (`agent.Backend` =
  vendor: claude / codex / …), so delegating to a codex-backed agent runs Codex.
- When the worker finishes it emits `⟢ done: <summary>`; `Report` seeds that back
  into the lead's turn — the async "I'll tell you when it's ready" round trip.
- The model triggers all of this with end-of-reply trailer markers
  (`⟢ delegate: <agent> — <task>`, `⟢ route: <task>`, …), parsed by
  `maybeCoordinate` / `handoff.go`.

Two gaps make this inert for the user's goal ("delegate to Codex when you can"):

1. **No consciousness.** A repo-wide grep confirms the `⟢` markers are injected
   **nowhere** in the prompt. The model is never told the capability exists — the
   exact same gap conscious-memory closed for the Obsidian vault.
2. **No Codex agent.** `~/.herrscher/agents` does not exist; no agent declares
   `backend=codex`, so `⟢ delegate: codex — …` fails with "unknown agent".

## Approach

Reuse the pattern shipped twice already (cross-backend skills menu, conscious-
memory framing): **inject a framed affordance into `Prompt.Context` at the bridge
seam**, listing the available agents and the delegation marker syntax. Plus ship
a default Codex agent so the capability is live out of the box.

### Decisions (approved)

- **Q1 — injection site: bridge (option A).** The affordance is assembled in
  `bridge.runOneTurn`, mirroring `withSkills` and the memory framing. Feasible
  because `bridge.Run(ctx, newBackend, orch, Options)` is constructed host-side
  where the agent roster (`deps.agents`) already lives, so a `RosterProvider` can
  be threaded through `bridge.Options`.
- **Q2 — Codex agent: default + manual (option A/B).** Auto-provision a `codex`
  agent on first run (idempotent, never overwriting an existing one), and keep the
  manual `agent create --backend codex` path.

## Components

### 1. `contracts.RosterProvider` — new read-only port

```go
// AgentInfo is the delegation-relevant projection of a roster agent.
type AgentInfo struct {
    Name    string   // agent name used in a ⟢ delegate: marker
    Backend string   // backend vendor it runs on (claude, codex, …); "" = host default
    Summary string   // one-line description for the menu (may be empty)
    Tags    []string // capability tags (also what ⟢ route: matches on)
}

// RosterProvider lists the agents a session may delegate to. Optional: a nil
// provider (or an empty roster) yields no delegation affordance.
type RosterProvider interface {
    Agents() []AgentInfo
}
```

Implemented host-side by a thin adapter over the existing `agent.Store.List()`.

### 2. Bridge framing — `withDelegation`

- `bridge.Options` gains `Roster contracts.RosterProvider` (optional).
- A new `withDelegation(memCtx string, roster RosterProvider) string` mirrors
  `withSkills`: it appends a `<delegation>` block to the context. `runOneTurn`
  composes them: `withDelegation(withSkills(memCtx, eng), roster)`.
- The block is compact and always-on (permanent awareness), e.g.:

  ```
  <delegation>
  You can hand a full mission to another agent — it runs autonomously in its own
  isolated worktree on its own backend and reports back to you when done (async;
  you keep talking meanwhile). End your reply with ONE trailer:
    ⟢ delegate: <agent> — <mission>   (spawn a worker, get its result back)
    ⟢ route: <mission>                (let the host pick the best-matching agent)
  When a worker's result lands in your turn, synthesize it for the human.
  Requires this session to be an isolated, committed worktree.
  Available agents:
    - codex (backend: codex) — mechanical refactors, test generation [refactor tests]
    - …
  </delegation>
  ```

- A nil/empty roster → the block is omitted entirely (`memCtx` returned
  unchanged), so a deployment with no agents is byte-for-byte as today.
- The block never mutates skills or memory context — it is appended after them.

### 3. Default Codex agent — idempotent provisioning

- On host start (where `agent.Store` is constructed), ensure a `codex` agent
  exists: if `Store.Get("codex")` is absent, create it via the existing
  provisioning path with `Backend: "codex"` and a small default tag set
  (`refactor`, `tests`, `mechanical`).
- Idempotent: an existing `codex` agent (manual or prior run) is left untouched —
  never overwrite the user's tags/cmd.
- The manual path (`agent create --backend codex`) is unchanged and documented.

## Data flow

```
turn N:  Context = memory + skills + <delegation menu (agents + markers)>
         model, now aware, ends its reply with "⟢ delegate: codex — <mission>"
         maybeCoordinate (host) parses → coordinator.Delegate → Codex worker
           spawned in an isolated worktree; lead keeps the turn (async)
         lead returns control to the human immediately
turn M:  Codex worker finishes → emits "⟢ done: <summary>" → Report seeds it into
         the lead's turn → lead synthesizes the result for the human
```

## Error handling

- Roster nil or empty → `<delegation>` omitted; nothing breaks (feature inert).
- Unknown agent / non-isolated (no worktree) / dirty source → already refused by
  the coordinator and surfaced as a `status` event (unchanged).
- Default-agent provisioning is best-effort and idempotent: an existing `codex`
  agent is a no-op; a provisioning failure logs and leaves the roster as-is
  (delegation simply lists whatever agents do exist).
- Marker parsing is unchanged (`handoff.go`), tolerant, last-line only.

## Testing

- `contracts`: a type satisfies `RosterProvider`.
- `bridge`: `withDelegation` lists agents (name · backend · tags) and the marker
  syntax; a nil roster leaves `Prompt.Context` unchanged; the delegation block
  composes with (does not clobber) skills + memory context.
- `host`: the `agent.Store → RosterProvider` adapter projects `AgentInfo`
  correctly; default-`codex` provisioning creates it when absent and is a no-op
  when present.
- Non-regression: `maybeCoordinate` / `coordinator` are untouched (already
  covered by their existing suites).

## Out of scope (YAGNI — separate follow-ups)

- Any new coordination logic — the whole `coordinator` (Delegate/Route/Report/
  Merge/Seal/FanOut/Handoff) already exists and is reused verbatim.
- `/resume` TUI discoverability/parity — its own spec.
- Smarter "which task suits which model" beyond the existing tag-based
  `⟢ route:` matching.
