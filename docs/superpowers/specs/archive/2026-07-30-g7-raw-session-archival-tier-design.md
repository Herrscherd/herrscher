# G7 — Raw-session archival tier (full-text searchable) — Design

**Status:** design (awaiting user review) · **Date:** 2026-07-30
**Serial order:** G6 ✅ → G4 ✅ → ★ ✅ → G5 ✅ → **G7 (this)** — the last Hermes-parity slice.

## Goal

Give herrscher a **raw-session archival tier**: index the raw per-turn
transcript for full-text recall — the analogue of Hermes' `state.db`/FTS5 raw
store — surfaced via a `memory search` verb. Today only *distilled* nodes (the
curator's facts/skills) are searchable; the verbatim conversation is lost to
recall once a session's rolling transcript scrolls past its bound.

**Acceptance (from the master design):** `memory search` returns hits from raw
transcripts, not just distilled nodes; ordinary recall is unchanged.

## Approved architecture decisions (user, 2026-07-30)

1. **Engine — reuse the obsidian vault.** Honor the master design's YAGNI line
   ("No new memory storage engine — ports + obsidian stay"): store raw per-turn
   chunks as nodes in the existing markdown vault and reuse the ranked `Search`.
   No SQLite/FTS5, no bleve, no cgo, no new dependency, no public-moat exposure.
   This is not a literal `state.db`, but it satisfies the acceptance criterion.
2. **Contracts surface — extend `contracts.Query`.** Add one opt-in field
   (`IncludeRaw`) plus one `NodeKind` (`KindTranscript`), mirroring the existing
   `IncludeArchived`/`StateArchived` precedent exactly. This is a *deliberate,
   minimal* relaxation of the "no contracts change" invariant the prior six
   slices held — chosen by the user — kept as small as the archived-node
   precedent that already lives in `Query`.
3. **Granularity — per-turn chunks.** Each turn is indexed as its own node, for
   finer-grained recall than a whole-session blob.

## The core invariant: the raw tier is invisible by default

The distilled recall path, and the orchestrator's `sweep`/`merge`/`promote`
passes, all call `Search`/`Recall` with a **default** `Query` (`IncludeRaw`
zero-value = false). The single rule

```
if n.Kind == KindTranscript && !q.IncludeRaw { return false }  // in matchesQuery
```

guarantees every existing caller is byte-for-byte unaffected: raw chunks never
pollute a distilled recall, never get swept, never get merged into an umbrella,
never get promoted across agents. The raw tier surfaces *only* when a caller
explicitly opts in (`IncludeRaw: true`), which only the new `memory search
--raw` path does. This is the same shape as `IncludeArchived` today.

## Components & data flow

### contracts (v0.2.9 → v0.2.10)

- `KindTranscript NodeKind = "transcript"` — a single raw turn chunk.
- `Query.IncludeRaw bool` — default false hides `KindTranscript` nodes from
  ordinary `Search`/`Recall`, exactly as `IncludeArchived` hides archived nodes.
  Documented as: raw archival tier, surfaced only on explicit opt-in.
- No new port, no new method. `Search` still returns `[]Node`.

### herrscher-obsidian-memory (v0.2.7 → v0.2.8)

- `matchesQuery`: add the default-hide rule above (raw hidden unless
  `q.IncludeRaw`). Placed alongside the existing archived-node gate.
- `Record`: **bypass the per-node rune budget** (`OBSIDIAN_NODE_BUDGET` /
  `EnforceBudget`) when `n.Kind == KindTranscript`. Archival must preserve the
  verbatim turn — truncating a raw chunk would defeat full-text recall. (Budget
  enforcement remains for every distilled kind.)
- Ranking already works: `Search` with `Ranked: true` scores raw hits via the
  existing `contracts.Score`; `Limit` takes the top-K. No ranker change.

### herrscher-orchestrator (v0.1.16 → v0.1.17)

- The write path lives in the learner's `Observe` (it already receives every
  `(prompt, reply)` turn and holds `mem` + `session` + `scope`). After the
  existing bounded-transcript upsert, when raw archival is enabled it does a
  **best-effort** `Record` of one raw node:
  - Key: `raw/<session-tail>/<seq>` (a per-session monotonic counter; append-only,
    never re-recorded, so no dedup/umbrella logic applies).
  - `Kind: KindTranscript`, `Title`: short (`turn <seq>`), `Body`: the verbatim
    turn (`author: <prompt>` + `\n\n` + `assistant: <reply>`), `Meta`: capturedAt.
  - Error is discarded (`_ =`) — **learning never breaks a turn** (invariant 2),
    same contract as the cadence `Consolidate` and the G5 idle pass.
- **Config toggle:** `raw-archive` / `MEMORY_RAW_ARCHIVE` (bool, default **off**).
  Opt-in because per-turn archival is write-heavy; off = no raw `Record`, no new
  vault growth, byte-for-byte prior behaviour. The Setting+Env+`cfg.Get` triple
  is wired in `register.go`'s Learner branch, like every prior slice's config.
- Raw nodes are provably untouched by `sweep`/`merge`/`promote`: those passes
  `Search` with default `Query`, so `IncludeRaw`=false hides `KindTranscript`.
  A regression test asserts each pass ignores a raw node.

### host (branch `herrscher-memory-review`)

- New verb `memory search`, mirroring `memory restore`'s registration shape:
  ```
  contracts.New("memory", "search").
    Help("full-text search the memory vault; --raw includes the raw transcript tier").
    Param("text", "query text", true).
    Param("raw", "include raw per-turn transcript chunks (G7 archival tier)", false).
    Param("limit", "max hits (default 10)", false).
    Do(... BuildFirstMemory → mem.Search(Query{Text, IncludeRaw: raw, Ranked: true, Limit}) → print key + title + snippet ...)
  ```
  Without `--raw`, it searches only distilled nodes (existing behaviour exposed
  as a CLI for the first time). With `--raw`, it adds the transcript tier. This
  verb is also the in-band surface the "Conscious memory" backends can drive.
- `go.mod`: bump contracts v0.2.10, obsidian v0.2.8, orchestrator v0.1.17;
  `go mod tidy`. README "Raw-session archival tier (G7)" paragraph in the
  Learning section. GOWORK=off release-parity verification vs published tags.

## Invariants (checklist for the plan's Global Constraints)

1. **Minimal contracts surface.** Exactly one `NodeKind` + one `bool`, shaped on
   the `IncludeArchived`/`StateArchived` precedent. No new port/method.
2. **Learning never breaks a turn.** Raw `Record` is best-effort, error
   discarded, and gated behind an opt-in toggle (default off).
3. **Append-only & reversible.** The raw tier is never truncated (budget bypass),
   never mutated in place (monotonic keys, no re-record), never swept/merged/
   promoted. Nothing in G7 deletes or overwrites.
4. **Invisible by default.** Every existing caller uses a default `Query`, so the
   raw tier is hidden — distilled recall, sweep, merge, promote all unchanged.

## Out of scope (YAGNI)

- No SQLite/FTS5/bleve/any new engine or dependency.
- No automatic in-band injection of raw hits into every turn's context (the
  bounded rolling transcript already covers short-term continuity); raw recall is
  pull-only via `memory search`.
- No retention/GC policy for the raw tier in this slice (append-only; a future
  slice can add age-based pruning if the vault grows unbounded — flagged, not
  built).

## Repos, versions, order

contracts v0.2.10 → obsidian v0.2.8 → orchestrator v0.1.17 → host bump + verb +
README. Main agent owns all tag/push/network ops; moat repos (contracts,
obsidian, orchestrator) commit/tag/push on master; host commits on the branch
(PR #46), GitHub-side merge. go.work overlay for local cross-module dev;
GOWORK=off vs published tags for release parity.

## Acceptance tests

- `memory search --text "<phrase from a turn>" --raw` returns the raw chunk;
  without `--raw` it does not.
- Ordinary `Recall`/`Search` (default Query) never returns a `KindTranscript`
  node (regression-locked).
- `sweep`/`merge`/`promote` ignore raw nodes (each has a test with a raw node
  present).
- A raw chunk larger than `OBSIDIAN_NODE_BUDGET` is stored **untruncated**.
- `raw-archive` off ⇒ no raw node is written (prior behaviour preserved).
