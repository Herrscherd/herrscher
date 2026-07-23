# Conscious Memory — design

**Date:** 2026-07-23
**Status:** approved (design), pending spec review
**Goal:** Make every backend (claude, codex, cursor) *aware* it has a persistent
Obsidian memory it can actively **search** and **write to** — not just receive a
passive injected digest.

## Problem

Herrscher already injects the Obsidian memory into `Prompt.Context` every turn,
backend-agnostically (`orchestrator.Curator.Context` → `runOneTurn`). But:

1. **No framing.** The digest arrives as bare `## Title / body` blocks. Nothing
   tells the model *"this is your persistent memory across sessions; you can rely
   on it, search it, and add to it."* The model treats it as background text.
2. **No model-driven use.** Writes happen only via the auto `Observe` transcript
   and background extraction. The model has no way to *search* memory on demand or
   to *durably store* a fact it judges worth keeping.

The result: the memory exists but the model has no *consciousness* of it — "ça
sert à rien". The user's emphasis: it must be **a place they can go and search**.

## Approach

Reuse the exact pattern shipped for cross-backend skills: **framed injection +
a model-emitted marker detected at the `runOneTurn` seam**, with results surfaced
next turn (progressive disclosure). Because the orchestrator is backend-agnostic,
every backend gets this uniformly — there is **no opt-out** (unlike skills, no
backend has a native Obsidian-memory loader).

### Decisions (defaulted autonomously; vetoable at review)

- **Q1 — both markers now.** Ship `<recall>` (search) *and* `<remember>` (write)
  in this iteration. Search is the headline; write is its natural complement and
  reuses the same seam.
- **Q2 — compact framing every turn.** A ~1-line preamble is injected on every
  turn (permanent awareness is the whole point), not just the first.

## Components

### 1. Framing (conscious read) — `orchestrator.Curator.Context`

`Context` wraps its recalled digest in an explicit, compact preamble, e.g.:

```
<memory>
This is your persistent memory (session · project · agent), recalled across
sessions. Search it any time by emitting <recall>your query</recall>; its hits
arrive next turn. Store a durable fact with <remember>the fact</remember>.
{existing digest…}
{pending recall results from your last <recall>, if any}
</memory>
```

Empty memory (`mem == nil` or nothing recalled) → still emits the one-line
affordance so the model knows the capability exists, or "" when the orchestrator
has no memory at all (unchanged first-turn behaviour when there's truly nothing).

### 2. `<recall>query</recall>` — the search (headline)

- Marker regex mirrors skills' `useMarker`, case/space tolerant.
- Detected in `runOneTurn` after `Respond`, at the same point `<use-skill>` is.
- Runs `contracts.RecallRelevant(ctx, mem, scope, query, k)` (scoped, ranked) and
  falls back to `mem.Search(ctx, Query{Text: query, Ranked: true, Limit: k})` when
  no scope — top-k nodes.
- Results are **stashed on the Curator** and surfaced in the **next** `Context()`
  call, framed as "results of your last search" — the two-turn round trip, like
  skills expansion.
- The marker is **stripped from the delivered reply** (same fix as skills v0.1.30
  `Engine.Strip`), so the human never sees it.

### 3. `<remember>fact</remember>` — conscious write

- Detected at the same seam, executed **immediately**.
- Stored durably via `contracts.RecordShared(ctx, mem, scope, node)` when the
  session has a project scope (visible to future sessions/agents of that project);
  falls back to recording under the session node when there is no scope.
- Node: deterministic `Key` = `<project>/notes/<slug(fact head)>` so re-remembering
  the same fact updates in place (no duplicates); `Kind = KindDecision`;
  `Title` = first line of the fact; `Body` = the fact.
- A discreet confirmation is appended to the reply (e.g. a dim "· mémorisé" note)
  so the operator sees it happened; details in the plan.

### 4. Seam — `contracts.TurnReactor` (optional interface)

```go
// TurnReactor is an OPTIONAL Orchestrator capability: it reacts to the model's
// reply, handling in-band memory markers (<recall>/<remember>) and returning the
// reply with those markers stripped. Orchestrators that don't implement it are
// unaffected — the host type-asserts, exactly like SkillNative / ResumeAware.
type TurnReactor interface {
    React(ctx context.Context, reply string) string
}
```

- Implemented by the default `Curator`.
- `runOneTurn`: after `Respond` and the existing skills `Detect`/`Strip`, do
  `if tr, ok := orch.(contracts.TurnReactor); ok { out = tr.React(turnCtx, out) }`
  before `sink.Emit`. `Observe` still runs afterwards on the cleaned reply.
- Read side (framing + pending-recall surfacing) needs **no** new interface — it's
  folded into the existing `Context()`.

## Data flow

```
turn N:   Context() → framed digest (+ pending recall from N-1) → prompt
          model reply contains <recall>foo</recall> and/or <remember>bar</remember>
          React(reply): remember bar now (RecordShared) · search foo → stash hits
          strip markers → Emit clean reply → Observe(clean reply)
turn N+1: Context() → framed digest + "results of your last search: …(foo hits)"
```

## Error handling

- All memory calls are best-effort: a failed `Search`/`Record` never breaks the
  turn (log-and-continue), consistent with `Context`/`Observe` today.
- `mem == nil` → `React` is a no-op and framing degrades to the affordance line
  (or "").
- Marker parsing is tolerant; unknown/empty markers are ignored.
- Injected node Title/Body already defanged (`writeNode` collapses `→`); recall
  results reuse the same defanging (multi-writer shared memory is attacker-influenced).

## Testing

- `orchestrator`: `React` writes a `<remember>` via a fake Memory; `<recall>` runs
  a search and the next `Context()` surfaces the hits; markers stripped from the
  returned reply; nil-memory no-ops; unknown markers ignored.
- `contracts`: a type satisfies `TurnReactor`.
- `core/bridge`: `runOneTurn` calls `React` when the orchestrator implements it and
  the emitted reply is marker-free; a plain orchestrator is unaffected.

## Out of scope (YAGNI — dedicated follow-up)

- Deep `[[wikilinks]]` graph traversal / cross-scope linking beyond current
  `RecallScoped` + `RecallRelevant` depth. A later "memory graph" chantier.
- Per-marker scope selectors (`<remember scope="agent">`), summarisation of recall
  hits, semantic/embedding search.
