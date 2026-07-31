# G1 — Forced consolidation — design

**Date:** 2026-07-30
**Status:** Design (approved)
**Slice:** G1 (remaining half) of the Hermes-parity roadmap
(`docs/superpowers/specs/2026-07-30-memory-learning-hermes-parity-design.md`)
**Repos:** `herrscher-orchestrator` only (the `Learner` write side). No
`herrscher-contracts` or `herrscher-obsidian-memory` change.

## Goal

Make the write side **respond** to a budget refusal instead of dropping the
fact. G1's first half already shipped — `contracts.BudgetError` (v0.2.9) and the
obsidian per-node rune budget (v0.2.7) mean `Memory.Record` *refuses* a node
whose `Body` exceeds the budget. But nothing catches that error: today an
over-budget candidate from the `Learner` fails and is silently lost, and the
first such failure aborts the entire consolidation pass (dropping every later
candidate and skipping the staleness sweep).

This slice adds the Hermes "refuse → merge-now" behaviour: on a refusal the
`Learner` asks a pluggable **`Consolidator`** to shrink the candidate to fit and
retries the write; if no consolidator is configured (or it cannot shrink the
node enough) the candidate is logged and held on an in-memory retry queue rather
than dropped or truncated.

## Baseline (verified in code, 2026-07-30)

- `contracts.BudgetError{Key, Runes, Limit}` exists (`budget.go`) with
  `errors.As` support. `contracts.EnforceBudget` also exists (from G6 planning).
- obsidian `recordUnlocked` (`memory.go:129-131`) returns
  `&contracts.BudgetError{...}` when `m.budget > 0` and the body exceeds it;
  wired via `OBSIDIAN_NODE_BUDGET` (default 2000) in `register.go`.
- orchestrator does **not** reference `BudgetError` anywhere (`git grep` empty) —
  nothing catches it.
- `Learner.Consolidate` (`learner.go:72-101`): reads the journal tail once
  (`readJournalTail` advances `offset` by the whole tail **before** the persist
  loop), then for each candidate skips if `seen[key]`, else persists via
  `contracts.RecordShared`/`RecordPrivate`, then adds the key to `seen`. It does
  `return err` on the **first** persist error — aborting the batch and skipping
  the final `_ = l.Sweep(ctx)`.
- `Extractor` is the existing seam (`learner.go:23-25`): the closed moat
  implements it; this package owns only the seam + open plumbing.
- `contracts.RecordShared(ctx, m, s, n) error` / `RecordPrivate(...)` are the
  scope helpers the Learner calls.
- The orchestrator has **no logging infrastructure** (only `fmt.Fprintf` into a
  digest `strings.Builder`).

## Design

### 1. The `Consolidator` seam (new, in orchestrator)

Mirrors `Extractor` — a narrow interface whose real implementation is the closed
moat:

```go
// Consolidator shrinks an over-budget memory candidate so a refused Record can
// be retried instead of dropped (memory roadmap G1). The merge/summarise
// heuristics are the closed part of the moat; this package defines only the seam
// and the open plumbing (Learner) that drives it. The returned node's Body
// SHOULD fit the given rune limit; the Learner re-checks and, if it still does
// not fit, treats the attempt as failed (enqueues for a later pass).
type Consolidator interface {
    Consolidate(ctx context.Context, over contracts.Node, limit int) (contracts.Node, error)
}
```

`limit` is the budget carried by the `*BudgetError` (`be.Limit`), so the
consolidator knows the exact target without any config coupling.

### 2. Injection by type-assertion (no new constructor param)

The closed extractor typically implements both `Extract` and `Consolidate`, so
the `Learner` discovers the consolidator from its existing extractor:

```go
func (l *Learner) consolidator() (Consolidator, bool) {
    c, ok := l.extract.(Consolidator)
    return c, ok
}
```

`NewLearner`'s signature is unchanged. A `nil` extractor (the plain-Curator
fallback) yields no consolidator, and `Consolidate` is already a no-op in that
case, so nothing changes for non-learning deployments.

### 3. The catch — a budget-aware `persist` helper

A single helper wraps both scope writes and centralises the budget response, so
the main loop and the retry-queue drain share one code path:

```go
// persist records one candidate, responding to a budget refusal by trying the
// Consolidator (if any) and retrying once. Returns:
//   nil            — persisted (caller marks it seen)
//   errEnqueue     — refused for budget and not resolvable now (caller enqueues)
//   other error    — a non-budget Record failure (caller records as first-error)
func (l *Learner) persist(ctx context.Context, c Candidate) error
```

Behaviour:
1. Call `RecordShared`/`RecordPrivate` (chosen by `c.Private`).
2. Success → return `nil`.
3. If the error is a `*contracts.BudgetError`:
   - If a `Consolidator` is present, call
     `Consolidate(ctx, c.Node, be.Limit)`. If it returns a node whose Body is
     `<= be.Limit` runes (`utf8.RuneCountInString`), retry the same scope write
     **once**. Success → return `nil`.
   - Otherwise (no consolidator, consolidator error, or a still-too-large
     result): `slog.Warn(...)` with the key/runes/limit and return `errEnqueue`.
4. Any other (non-budget) error → return it unchanged.

`errEnqueue` is a package-private sentinel (`errors.New`) used only to signal the
caller; it is never returned out of `Consolidate`.

### 4. The retry queue

The `Learner` gains an in-memory, per-session pending set:

```go
pending []Candidate // candidates refused for budget, retried on later passes
```

- **On `errEnqueue`:** append the candidate unless a candidate with the same
  `Node.Key` is already pending (dedup keeps the queue bounded). Never added to
  `seen` (it was never persisted).
- **At the top of each `Consolidate` pass (before reading the journal tail):**
  drain `pending` through `persist`. Anything that now succeeds is removed and
  its key added to `seen`; anything still refused stays. This is where a
  previously-queued fact gets written once a consolidator is available or the
  budget changes.
- The queue is deliberately **in-memory only** — a chronically-unmergeable fact
  is retried for the life of the session and then dropped on restart (the raw
  journal on disk remains the source of record). Persisting the queue across
  restarts is out of scope (YAGNI); this limitation is called out here rather
  than silently accepted.

### 5. No-abort batch (robustness fix)

The persist loop no longer aborts on the first error. It records the **first**
non-budget error and continues, so one bad candidate can neither drop its
siblings nor skip the sweep — the same best-effort shape already applied to
`Sweep`. `Consolidate` returns that first error (still swallowed by `Observe`'s
`_ = l.Consolidate(ctx)`), and the final `_ = l.Sweep(ctx)` always runs.

## Testing

All in `herrscher-orchestrator`, using a fake `Memory` that returns
`*contracts.BudgetError` for bodies over a set limit, and fake extractor/
consolidator types.

- **Consolidator resolves a refusal:** a candidate over budget + a consolidator
  that shrinks it to fit → the node is persisted (fake records the shrunk body),
  key is `seen`, queue empty.
- **No consolidator → enqueue + WARN + siblings survive:** a batch of
  [over-budget, under-budget] with no consolidator → the under-budget node
  persists, the over-budget one is queued (not persisted, not `seen`), and the
  sweep still runs (assert via a sweep-observable side effect or a spy).
- **Still-too-large result → enqueue:** consolidator returns a node still over
  `limit` → treated as failure, candidate queued, not persisted.
- **Pending drained on a later pass:** enqueue with no consolidator, then a
  second `Consolidate` with a consolidator present → the queued candidate
  persists and leaves the queue.
- **Dedup bounds the queue:** the same over-budget key refused across two passes
  appears in `pending` at most once.
- **Non-budget error does not abort the batch:** a fake that fails one candidate
  with a generic error and succeeds the next → the second still persists;
  `Consolidate` returns the first error; the sweep still runs.

## Release footprint

- `herrscher-orchestrator` → **v0.1.10** (adds `Consolidator` + retry queue +
  no-abort fix). Depends only on the already-released contracts v0.2.9.
- host `go.mod`: bump orchestrator v0.1.9 → v0.1.10; `GOWORK=off` build + full
  test. No contracts/obsidian change.

Per the roadmap, all modules imported by the public host must be public tags —
tag and push orchestrator, then bump the host.

## Out of scope (YAGNI)

- **Persisting the retry queue across restarts** — in-memory only this slice;
  the journal on disk is the durable source.
- **A per-scope aggregate budget** (bounding a whole `projects/<project>` graph,
  not just one node) — a separate future slice if needed; per-node enforcement +
  forced consolidation is the atomicity forcer G1's rationale calls for.
- **The concrete merge/summarise heuristics** — the closed `Consolidator` impl;
  this repo ships only the seam + a deterministic no-consolidator fallback.
- **A new env flag to toggle forced consolidation** — the presence of a
  `Consolidator` (and a non-zero obsidian budget) is the switch; no new config.

## Invariants (from the umbrella roadmap)

1. **Ports only, policy not engine** — forced consolidation is a seam
   (`Consolidator`) plus plumbing that drives the existing `Memory` port; no new
   storage engine, no new `contracts.Memory` method.
2. **Learning never breaks a turn** — `persist`, the queue drain, and the
   consolidator call are all inside the best-effort `Consolidate`, whose result
   is swallowed by `Observe`; a consolidator panic-free error never propagates to
   the turn loop.
3. **Reversible over destructive** — a refused candidate is merged-to-fit or held
   for retry, never truncated in place or hard-deleted; the raw journal remains.
