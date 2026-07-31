# Memory & Learning — Hermes Parity + Beyond

**Date:** 2026-07-30
**Status:** Design (umbrella roadmap)
**Scope:** herrscher agent-memory / learning stack across `herrscher-contracts`,
`herrscher-orchestrator`, `herrscher-obsidian-memory`, and the host.

## Goal

Bring herrscher's persistent-agent memory to parity with Nous Research **Hermes**
(`hermes-agent.nousresearch.com/docs`), then exceed it on the one axis where
herrscher's architecture is already stronger: a **multi-agent scoped graph** vs
Hermes' flat, single-agent files.

This is an **umbrella roadmap**. Each gap below is an independently shippable
slice with its own spec → plan → implementation cycle. This document fixes the
sequencing, the slice boundaries, the target repo(s), and the acceptance test
for each. It does **not** design any single slice in full — that happens when we
pick the slice up.

## Non-negotiable invariants (apply to every slice)

1. **Ports only, policy not engine.** Everything routes through the existing
   `contracts.Memory` and `contracts.CurationHook` ports. The core gains
   *policy* (budgets, scoping, staleness rules); it does not gain a new memory
   engine. Storage stays in plugins (`obsidian-memory`), curation logic stays in
   `herrscher-orchestrator`.
2. **Learning never breaks a turn.** Every slice preserves the Learner's
   existing property: consolidation runs **out-of-band / best-effort**, so a
   failure in the learning path never fails or blocks a user turn.
3. **Reversible over destructive.** No slice may hard-delete a user fact/skill
   without an archive + restore path (established by G4, then required
   thereafter).

## Baseline — what already exists (verified in code, 2026-07-30)

- **Durable Agent** — `core/internal/agent/`: home holding `SOUL.md`,
  `mcp.json`, `settings.json`; `Agent.Materialize` copies them into the session
  worktree (Claude Code auto-reads `.claude/CLAUDE.md`, `.mcp.json`,
  `.claude/settings.json` when cwd = worktree).
- **Skills** — `core/skills/`: `SKILL.md` discovery + progressive disclosure
  (per-turn menu of name+description; `<use-skill>NAME</use-skill>` expands the
  body). This is Hermes' procedural layer.
- **Memory graph** — port `contracts.Memory` (`Recall/Record/Search/Links`),
  obsidian impl (markdown + frontmatter + `[[wikilinks]]`). Scoping already
  exists: shared `projects/<project>` vs private `agents/<agent>` via
  `contracts.MemoryScope`.
- **Learner** — `herrscher-orchestrator/learner.go`: runs an `Extractor` over
  the journal **tail** (byte-offset tracked → O(new bytes), not O(total)),
  persists shared facts + private skills, idempotent per session, cadence
  `memory.consolidate-every` N turns, out-of-band. Default `Curator.Consolidate`
  is a no-op (bounded rolling transcript kept inline).

## Hermes reference (what we are matching)

- `MEMORY.md` ~800 tok / **2200 char hard limit**; `USER.md` ~500 tok / 1375
  char. Writes over the limit are **refused** with "consolidate now" → merge via
  `replace`. "Frozen snapshot": injected once at session start.
- `state.db` (SQLite **FTS5**) for raw cross-session recall.
- Skills = separate procedural layer (`SKILL.md`, YAML frontmatter, versioned).
- **Curator**: inactivity trigger (≥7 days since last run **and** idle ≥2h) →
  background fork with own prompt cache; staleness state machine
  active↔stale↔archived; LLM consolidation into umbrellas; reversible archive +
  restore; per-run `REPORT.md`.

## The roadmap — 7 gaps + the differentiator

Ordering rationale: each slice unblocks the next, and each is valuable alone.
G1 first because a budget forces atomicity **at write time** — without it every
downstream curator merely chases an unbounded leak.

### Slice 1 — G1: Budget + forced consolidation
- **Repos:** `herrscher-contracts` (declare a budget on the memory port /
  scope), `herrscher-obsidian-memory` (enforce at `Record`).
- **Behavior:** each memory node (and the per-scope aggregate) carries a soft
  char/token budget. `Record` that would exceed it returns a typed
  "budget exceeded — consolidate" error instead of silently appending; the
  caller (Learner or a CLI verb) must merge/replace to fit.
- **Why here:** foundation; forces atomicity, bounds everything downstream.
- **Accept:** unit test — `Record` past budget returns the typed error; a
  merge-then-record succeeds; existing Learner path still green.

### Slice 2 — G6: Bounded `USER.md`
- **Repos:** `core/internal/agent` (home + materialize), `herrscher-contracts`.
- **Behavior:** add `USER.md` (identity/preferences) to the agent home,
  separate from project facts, materialized into the worktree, bounded by the
  G1 budget.
- **Why here:** small; isolates identity from facts; reuses G1.
- **Accept:** `USER.md` present in home, copied on `Materialize`, budget enforced.

### Slice 3 — G3: Staleness state machine
- **Repos:** `herrscher-orchestrator` (`Curator`), `herrscher-contracts` (node
  `Meta` gains `state` + `reviewed`/`last-seen`).
- **Behavior:** pure, time-based transitions `active → stale → archived` (and
  reactivation on re-observation). No LLM — deterministic and unit-testable.
- **Why here:** decay is the core drift fix; relies on bounded nodes from G1.
- **Accept:** test drives clock/last-seen and asserts transitions; no LLM call.

### Slice 4 — G4: Reversible archive + REPORT
- **Repos:** `herrscher-orchestrator`, `herrscher-obsidian-memory`.
- **Behavior:** archiving (from G3) moves nodes to an `archive/` area instead of
  deleting; a per-run `REPORT.md` records transitions; a `memory restore` verb
  brings a node back.
- **Why here:** makes G3 safe to run; adds the audit trail Hermes has.
- **Accept:** archived node is recoverable; REPORT written; restore verb works.

### Slice 5 — G5: Inactivity-triggered curator
- **Repos:** `herrscher-orchestrator` (trigger), host (`turnloop`/supervisor
  wiring).
- **Behavior:** in addition to the per-turn cadence, a curator run fires on
  inactivity (config: days-since-last-run + idle threshold), out-of-band, à la
  Hermes' background fork.
- **Why here:** needs G3/G4 to have real work to do.
- **Accept:** simulated clock/idle triggers a Consolidate off the turn loop;
  turn loop unaffected.

### Slice 6 — G2: Semantic merge (umbrellas)
- **Repos:** `herrscher-orchestrator` (Extractor/Learner), an LLM backend for
  the merge pass.
- **Behavior:** an LLM pass fuses semantically overlapping nodes into an
  "umbrella" node, idempotently; links the merged originals.
- **Why here:** the "smartest" step; needs a budget (G1) to motivate merging and
  staleness (G3) to target candidates.
- **Accept:** overlapping fixtures collapse to one umbrella; re-running is a
  no-op (idempotent).

### Slice 7 — G7: Raw-session archival tier (FTS)
- **Repos:** `herrscher-obsidian-memory` (or a new plugin).
- **Behavior:** index the raw session transcript for full-text recall (Hermes
  `state.db`/FTS5 analogue), surfaced via `memory search`.
- **Why here:** independent, least urgent; distilled memory already works
  without it.
- **Accept:** `memory search` returns hits from raw transcripts, not just nodes.

### Slice ★ — Cross-agent promotion (the "beyond Hermes")
- **Repos:** `herrscher-orchestrator` (scope policy).
- **Behavior:** the curator, seeing all agents in the shared graph, **promotes**
  a proven private skill (`agents/<a>`) to the shared project node
  (`projects/<p>`) so peer agents inherit it. Hermes (flat, mono-agent) cannot
  do this.
- **Why here:** natural once G3 (proven/active) + G5 (curator runs) exist.
- **Accept:** a private skill meeting a promotion rule appears under the shared
  scope after a curator run; scoping/topology tests pass.

## Out of scope (YAGNI)

- No new memory storage engine (ports + obsidian stay).
- No rewrite of the Skills progressive-disclosure engine (already sufficient).
- No UI/dashboard for the curator beyond the `REPORT.md` artifact.
- Model choice for the G2/G5 LLM passes is deferred to those slices' specs.

## First implementation target

**Slice 1 (G1)** — its spec + plan come next. Everything else is sequenced
behind it per the table above.
