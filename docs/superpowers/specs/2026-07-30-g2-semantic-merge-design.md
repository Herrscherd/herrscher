# G2 — Semantic merge (umbrellas) — design

**Date:** 2026-07-30
**Status:** Design (approved)
**Slice:** G2 of the Hermes-parity roadmap
(`docs/superpowers/specs/2026-07-30-memory-learning-hermes-parity-design.md`)
**Repos:** `herrscher-orchestrator` only. No `herrscher-contracts`, no
`herrscher-obsidian-memory` change.

## Goal

Fuse semantically overlapping memory nodes into a single **umbrella** node, so
the graph compacts instead of accumulating near-duplicate fragments forever.
herrscher today only dedups by exact session key (the `seen` set); two nodes
that *say the same thing* under different keys both survive. G2 adds the "smart"
compaction step the roadmap calls for — the pass that a budget (G1) motivates and
staleness (G3) targets.

The **merge heuristic** — which nodes overlap, how to phrase the fused body, how
to name the umbrella — is the **closed part of the moat**, an LLM pass. This repo
ships only the **open plumbing**: a deterministic candidate pre-filter, the seam
the closed merger implements, the curator loop that applies its output
reversibly, and the config surface. Without a merger wired, G2 is a clean no-op.

## Baseline (verified in code, 2026-07-30)

- `Learner` (`learner.go`) owns `extract Extractor`; seams are discovered by
  type-asserting it (`l.extract.(Consolidator)` shipped in G1 v0.1.10). `Merge`
  will live on `*Learner` for the same reason.
- `Curator` (`orchestrator.go`) owns `mem`, `scope`, `now`, `staleAfter`,
  `archiveAfter`; `Sweep` (`sweep.go`) is a `*Curator` method that
  `Search(Query{IncludeArchived:true})`es the whole vault, re-derives each node's
  lifecycle state, and best-effort `Record`s changes. `Consolidate` ends with
  `_ = l.Sweep(ctx)`.
- `contracts.Query` has no state filter; `Sweep` filters in Go on
  `Meta[MetaState]`. `Search` without `IncludeArchived` already excludes
  `StateArchived` nodes (G3) — this is what hides merged originals from recall.
- `contracts.Node` carries `Meta["domain"]` by convention (per the Node doc
  comment in contracts) — a fact's domain, used here to avoid merging across
  unrelated domains.
- Config pattern: `contracts.Setting{Key, Env, Help}` in the plugin `Manifest`,
  read in the `Orchestrator` factory via `cfg.Get(...)` and pushed through a
  setter (e.g. `SetStaleness`). The Learner branch (extractor present) is the
  only place a merger can exist, so `SetMerge` is called there.

## Design

### 1. The `Merger` seam + `Umbrella` type (new, in orchestrator)

```go
// Umbrella is one merge proposal from a Merger: a fused node that subsumes the
// originals named by Merged (their Keys). The plumbing writes Node, then labels,
// links, and archives each original. The fused node's Key/Title/Body/Meta and
// the overlap decision are the closed merger's to make; this package only
// validates and applies.
type Umbrella struct {
    Node   contracts.Node
    Merged []string // Keys of the originals this umbrella subsumes (>= 2)
}

// Merger fuses semantically overlapping candidates into umbrella nodes (memory
// roadmap G2). Given a pre-filtered, single-domain slice of candidates it
// returns zero or more umbrellas; an empty result means "nothing worth merging".
// The heuristics are the closed part of the moat; this package defines only the
// seam and the open plumbing (Learner.Merge) that drives it.
type Merger interface {
    Merge(ctx context.Context, cands []contracts.Node) ([]Umbrella, error)
}
```

Discovery mirrors G1: `func (l *Learner) merger() (Merger, bool) { m, ok := l.extract.(Merger); return m, ok }`. The closed extractor typically implements `Extract`, `Consolidate`, and `Merge`.

### 2. The `mergedInto` label (orchestrator-internal)

```go
// MetaMergedInto, when set on a node, names the umbrella Key that subsumed it.
// It is a terminal marker: the node is kept on disk (reversible) but archived
// and excluded from recall. Orchestrator-internal — obsidian stores Meta
// generically, so no contracts change is needed.
const MetaMergedInto = "mergedInto"
```

### 3. The curator loop `Merge(ctx)` — best-effort, on `*Learner`

Runs at the end of `Consolidate`, **after** `Sweep`:

```go
_ = l.Sweep(ctx)
_ = l.Merge(ctx)   // best-effort: a merge error never breaks the turn
return firstErr
```

`Merge` steps:
1. If `l.mergeMin <= 0` → return nil (opt-in; default off). If no `merger()` →
   return nil.
2. `Search(Query{})` (active + stale, never archived).
3. Filter in Go by `l.mergeTarget`: `stale` (default — only
   `Meta[MetaState] == StateStale`), `active` (only active/absent-state), or
   `all` (both). Skip any node already carrying `MetaMergedInto` (defensive; such
   nodes are archived and already excluded by Search).
4. Group the survivors by `Meta["domain"]` (missing domain → the `""` group).
5. For each group with `len(group) >= l.mergeMin`, cap it to the first
   `l.mergeMax` nodes and call `merger.Merge(ctx, group)`.
6. Apply each returned `Umbrella` through `applyUmbrella` (below). A merger error
   for one group records the first error and continues to the next group.

Returns the first error encountered (swallowed by `Consolidate`).

### 4. `applyUmbrella` — reversible write + validation guard

Mirrors G1's consolidator-result guard. For one `Umbrella u` against the
candidate group it came from:

- **Reject (WARN + skip, keep going) if:** `u.Node.Key == ""`; `u.Node.Body ==
  ""`; `len(u.Merged) < 2`; any key in `u.Merged` is not in the group; `u.Node.Key`
  is itself one of `u.Merged` or an already-existing node key (an umbrella is a
  *new* node, never an original reused). A bad proposal must not corrupt the graph
  or drop the good proposals.
- **Apply if valid:**
  1. `Record(u.Node)` — write the umbrella.
  2. For each original key `k` in `u.Merged`: load it from the group, set
     `Meta[MetaMergedInto] = u.Node.Key` and `Meta[MetaState] = StateArchived`,
     re-supply its existing `MetaLastSeen` (so the archive write does not bump its
     age), `Record` it, then `Links(k, u.Node.Key, "merged-into")`.
  - Per-original write failures are best-effort: record the first, continue (one
    bad original must not strand the rest or the umbrella).

### 5. Sweep interaction (correctness — invariant 3)

`Sweep` must **skip** any node with `Meta[MetaMergedInto] != ""`:

```go
for _, n := range nodes {
    if n.Meta[MetaMergedInto] != "" { // orchestrator-local const (§2)
        continue // merged originals are terminal; never reactivate a fragment
    }
    ...
}
```

Without this, a re-observed merged original (bumped
`lastSeen`) would transition `archived → active` and re-surface a fragment we
deliberately folded into its umbrella.

### 6. Config surface

Three new `Setting`s in the orchestrator manifest, read in the factory's Learner
branch and pushed via a new `SetMerge`:

| Setting key | Env | Default | Meaning |
|---|---|---|---|
| `memory.merge-min-nodes` | `MEMORY_MERGE_MIN` | `0` (off) | min candidates in a domain group to wake the merger; `0` disables G2 cleanly. |
| `memory.merge-target` | `MEMORY_MERGE_TARGET` | `stale` | which nodes to consider: `stale` / `active` / `all`. Unknown value → `stale`. |
| `memory.merge-max` | `MEMORY_MERGE_MAX` | `40` | cap on nodes handed to the merger per group (bounds LLM cost / prompt size). `<=0` → default 40. |

```go
// SetMerge configures the G2 merge pass. minNodes <= 0 disables the pass.
func (l *Learner) SetMerge(minNodes, max int, target string)
```

Fields on `Learner`: `mergeMin int`, `mergeMax int`, `mergeTarget string`.
Defaults when `SetMerge` is never called (plain Curator / no extractor): `mergeMin
== 0` → off.

## Idempotence

- Merged originals are archived → excluded from the next pass's `Search`.
- A freshly-written umbrella is active; with the default `target=stale` it is not
  re-considered until it goes stale. Two umbrellas that later overlap *may*
  re-merge — intended (the pass is idempotent, not one-shot).
- No `seen`-style tracking is needed: the archived+labeled state of originals is
  the idempotence record, and it is durable on disk.

## Testing (orchestrator, fake `Memory` + fake `Merger`)

- **No merger → no-op:** a plain `Extractor` (no `Merge` method); `Merge` writes
  nothing.
- **`mergeMin == 0` → no-op:** merger present but disabled; no merger call.
- **Below threshold → no call:** a domain group smaller than `mergeMin` never
  reaches the merger (spy asserts zero calls).
- **Happy path:** a group ≥ threshold → umbrella recorded; each original labeled
  `mergedInto`, set `StateArchived`, and linked `merged-into` → umbrella; original
  `lastSeen` unchanged by the archive write.
- **Validation guard:** a returned umbrella with `<2` Merged / empty Key / empty
  Body / a Merged key outside the group / a reused existing key is rejected; a
  valid umbrella in the same batch still applies.
- **Domain isolation:** two domains each below threshold but jointly above it are
  never merged together (grouped separately).
- **Cap respected:** a group larger than `mergeMax` hands the merger exactly
  `mergeMax` nodes.
- **Target filter:** `target=stale` hands the merger only stale nodes; active
  ones are excluded.
- **Sweep skips merged originals:** a node bearing `MetaMergedInto` with a fresh
  `lastSeen` is NOT reactivated by `Sweep` (stays archived).
- **Best-effort:** a merger error on one group does not abort other groups; a
  per-original `Record` error does not strand the umbrella or siblings.

## Release footprint

- `herrscher-orchestrator` → **v0.1.11** (Merger seam + Merge loop + Sweep guard
  + config). Depends only on the already-released contracts v0.2.9.
- host `go.mod`: bump orchestrator v0.1.10 → v0.1.11; `GOWORK=off` build + full
  test against real tags. No contracts/obsidian change.
- README "Learning (the write side)" gains a **Semantic merge** paragraph; the
  memory-vs-hermes note marks G2 shipped.

## Out of scope (YAGNI)

- **A dedicated `merge-every` cadence / counter** — the pass is coupled to
  `Consolidate` (after `Sweep`); `merge-min-nodes` is the cost gate. Add later if
  a decoupled cadence is ever needed.
- **Cross-scope isolation beyond domain grouping** — grouping is by
  `Meta["domain"]`; finer scope separation is delegated to the closed merger
  (which sees the `projects/` vs `agents/` key prefixes). A structural scope
  filter is a later refinement if needed.
- **The concrete merge/overlap heuristics and umbrella phrasing** — the closed
  `Merger` impl; this repo ships the seam + a deterministic no-merger no-op.
- **Un-merge / restore command** — reversibility is preserved (originals kept +
  labeled), but an explicit restore flow belongs to G4 (reversible archive).

## Invariants (from the umbrella roadmap)

1. **Ports only, policy not engine** — G2 is a seam (`Merger`) plus plumbing over
   the existing `Memory` port; no new `contracts.Memory` method, no new storage
   engine, no contracts change at all.
2. **Learning never breaks a turn** — `Merge` is best-effort inside `Consolidate`
   (`_ = l.Merge(ctx)`), whose result is swallowed by `Observe`; a merger panic-
   free error never propagates to the turn loop, and the sweep already ran.
3. **Reversible over destructive** — originals are labeled + archived + linked,
   never deleted or overwritten in place; the umbrella is a new node, and the
   `mergedInto` label makes the fold fully reversible.
