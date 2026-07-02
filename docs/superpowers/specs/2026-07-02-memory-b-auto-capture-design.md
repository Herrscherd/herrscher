# Memory B — Auto-capture (curation loop)

**Date:** 2026-07-02
**Status:** Design (approved for spec)
**PR order:** 2 of 4 (C → B → A → D)
**Repos touched:** `herrscher-orchestrator`, `herrscher-contracts` (minor), `core`

## Context

This is the heart of "herrscher gère sa propre mémoire de souvenirs tout seul."
Today nothing writes to memory autonomously, so the vault stays empty.

The seam already exists:
- `contracts.Orchestrator` (`orchestrator.go`) is session-scoped, composes the
  `Memory` port, and receives every completed turn via
  `Observe(ctx, Prompt, reply)`. So it can accumulate the session transcript.
- `CurationHook.Consolidate(ctx)` is the declared "proactive curation" seam. The
  default orchestrator no-ops it.
- The host calls `orch.Observe(...)` after each turn (`core/bridge/hub.go:95`).

What is missing: an implementation of `Consolidate` that reads the accumulated
transcript, extracts salient facts, and writes them into the graph via the P1
scope helpers (`RecordShared`/`RecordPrivate` in `memory_scope.go`).

## Goal

herrscher autonomously turns a finished session into durable memory nodes:
extract → dedupe → record, with provenance for human audit.

## Decisions (approved defaults)

- **Trigger:** on **session close** (batched, one extraction per session), not
  per-turn. Cheap and coherent — the whole conversation is in view.
- **Extraction:** **LLM-driven**, not heuristic. This is the moat's quality edge.
- **Write autonomy:** **auto-write** above a confidence threshold, no human
  approval queue. Provenance (`Meta["originSessionId"]`) makes writes auditable
  and prunable after the fact — mirrors how Claude's own memory works.

## Design

### 1. Curation backend (the one real architectural addition)

Extraction needs a model call. The orchestrator today composes `Memory` but not
`Backend`. Add an **optional curation backend** to the orchestrator factory's
`PluginConfig` composition (a `contracts.Backend`), used **only** by
`Consolidate` — separate from the per-turn conversation backend so curation can
run on a cheaper/faster model and never blocks a turn.

- If no curation backend is wired, `Consolidate` degrades to a no-op (same as
  today) — memory still works for recall, it just doesn't self-populate.
- Reuse the existing `contracts.Backend` port (`Respond(ctx, Prompt, onEvent)`);
  no new port. The extraction prompt goes in `Prompt.Content`; the transcript in
  `Prompt.Content` too (or `Context`), reply parsed as JSON.

### 2. The Consolidate flow

```
Consolidate(ctx):
  transcript := orchestrator's rolling transcript (already kept for Context/Observe)
  if transcript empty OR no curation backend: return nil
  raw := curationBackend.Respond(ctx, extractionPrompt(transcript))
  candidates := parseCandidates(raw)            // []Candidate, tolerant parse
  for c in candidates:
    if c.Confidence < threshold: continue
    if isDuplicate(ctx, memory, c): continue     // Search by title/text, skip near-dupes
    node := toNode(c)                             // Kind, Title, Body, Links, Meta
    node.Meta["originSessionId"] = session
    node.Meta["capturedAt"] = <the run's timestamp, injected>
    if c.Private: RecordPrivate(ctx, memory, scope, node)
    else:         RecordShared(ctx, memory, scope, node)
  return nil   // best-effort; never breaks anything (Consolidate is out-of-band)
```

### 3. Extraction contract (prompt → JSON)

The extraction prompt instructs the model to return a JSON array of candidates:

```json
[{
  "kind": "decision|architecture|user|production|...",
  "title": "short stable title",
  "body": "the fact in markdown; why it matters; how to apply",
  "domain": "dev",                     // optional, feeds chantier C routing
  "tags": ["nats","transport"],
  "links": [{"to":"projets/x/index","rel":"applies-to"}],
  "private": false,                    // true → RecordPrivate (agent-scoped)
  "confidence": 0.0-1.0
}]
```

`Candidate` is a small struct local to the orchestrator (not a new contract).
Parsing is tolerant: a malformed element is skipped, never fatal. The prompt
mirrors the memory-writing guidance herrscher already knows (one fact per node,
`**Why:** / **How to apply:**` for feedback/decision kinds, link liberally).

### 4. Deduplication

Before recording, `Search(Query{Text: candidate.Title})` (and/or by Kind). If a
node with a very similar title/body exists, skip (or `Record` to upsert the same
Key when the model reuses a key — `Record` is already upsert-by-Key). Keeps the
vault from accreting near-duplicates across sessions.

### 5. Wiring in core

`core/bridge` builds the orchestrator per session. On session **close**, the
host must call `orch.Consolidate(ctx)` before `orch.Close()`. Confirm/adds the
close-time hook next to where sessions tear down (the worktree removal path).
Consolidate is best-effort and time-boxed so a slow model can't hang teardown.

## Interfaces changed

| Symbol | Repo | Change |
|--------|------|--------|
| orchestrator factory | orchestrator | accepts optional curation `Backend` via PluginConfig |
| `Consolidate` impl | orchestrator | new: extract→dedupe→record |
| session close | core | calls `Consolidate` before `Close`, time-boxed |

No contract-breaking change: `CurationHook` already exists; the curation backend
is composed through the existing plugin-config mechanism.

## Testing

- `Consolidate` with a fake curation backend returning 2 candidates records 2
  nodes with `originSessionId` set, scoped shared/private per `private`.
- Candidate below threshold is dropped.
- Duplicate title → no second node.
- Malformed JSON element → skipped, others still recorded, no error returned.
- No curation backend → `Consolidate` is a no-op returning nil.
- Core: session close invokes `Consolidate` exactly once and does not fail
  teardown if it errors/times out.

## Open questions (non-blocking)

- Model for the curation backend: default to a fast/cheap Claude model; make it
  configurable via the plugin manifest.
- Timestamp injection: scripts/tests must inject the clock (no `time.Now()` in a
  deterministically-tested path) — pass `capturedAt` from core.
