# G3 — Staleness state machine — design

**Date:** 2026-07-30
**Status:** Design (approved)
**Slice:** G3 of the Hermes-parity roadmap
(`docs/superpowers/specs/2026-07-30-memory-learning-hermes-parity-design.md`)
**Repos:** `herrscher-contracts` (state vocabulary + pure transition),
`herrscher-obsidian-memory` (lastSeen stamp + archived exclusion),
`herrscher-orchestrator` (the sweep + injectable clock), host (env config).

## Goal

Give every memory node a deterministic, time-based lifecycle
`active → stale → archived`, with automatic reactivation when a node is
re-observed. Pure and unit-testable: no LLM, no wall-clock reads inside the
decision function. This is the decay mechanism that keeps the graph honest —
old, unrefreshed facts fade instead of accumulating forever.

Per the umbrella invariant **reversible over destructive**, G3 only *labels*
state. Moving archived nodes to an `archive/` area, writing a `REPORT.md`, and
a `restore` verb belong to **G4** and are out of scope here.

## Baseline (verified in code, 2026-07-30)

- `contracts.Node` (`memory.go:40-47`) carries `Meta map[string]string` — fully
  generic; obsidian's frontmatter marshaller (`vault.go`) round-trips any Meta
  key with no code change. `type`/`title` are the only reserved keys.
- `Meta["capturedAt"]` is stamped by obsidian's `writeNode` on first write and
  **preserved on every upsert** (`memory.go:149-165`; pinned by
  `memory_test.go:311-312`). It therefore cannot serve as "last seen".
- `ObsidianMemory.now func() time.Time` (`memory.go:40`, set in `New`,
  overridden in tests) is an already-injectable clock, currently used only for
  `capturedAt` stamping and `Search` recency ranking.
- `Search(ctx, Query{})` walks all `.md` files and returns every node when the
  query has no filters (`memory.go:279-337` + `matchesQuery`). This is the
  sweep's enumeration primitive.
- `Query` (`memory.go`) has fields `Text, Kinds, Tags, Limit, Ranked` — no
  age/state filter today.
- `Curator.Consolidate` default is a literal no-op (`orchestrator.go:121`);
  `Learner.Consolidate` (`learner.go:72-101`) is the real one. The orchestrator
  repo contains **no** `time.Now`/clock usage at all.
- No node-state machine, no "archived" concept, no `lastSeen`/`reviewed` field,
  and no scheduled curation exist anywhere in the three repos. Only prior art:
  `capturedAt` + `scoring.go` read-side recency decay (ranking only).

## Design

### 1. contracts — state vocabulary + pure transition (v0.2.9, additive)

New file `state.go`:

```go
// Node lifecycle states, stored in Node.Meta[MetaState]. An absent value is
// treated as StateActive.
const (
    StateActive   = "active"
    StateStale    = "stale"
    StateArchived = "archived"
)

// Reserved Meta keys for the staleness machine.
const (
    MetaState    = "state"
    MetaLastSeen = "lastSeen"
)

// NextState derives a node's lifecycle state purely from how long ago it was
// last seen. age = now.Sub(lastSeen). A duration <= 0 disables that step:
// staleAfter <= 0 means nodes never become stale; archiveAfter <= 0 means they
// never become archived. When both are set, archiveAfter should exceed
// staleAfter; if archiveAfter <= staleAfter, archival still wins once its
// threshold is crossed. The current state is intentionally not an input:
// transitions (including reactivation) depend only on age, so the function is
// total and hysteresis-free.
func NextState(lastSeen, now time.Time, staleAfter, archiveAfter time.Duration) string {
    age := now.Sub(lastSeen)
    if archiveAfter > 0 && age >= archiveAfter {
        return StateArchived
    }
    if staleAfter > 0 && age >= staleAfter {
        return StateStale
    }
    return StateActive
}
```

`Query` gains one field:

```go
// IncludeArchived includes nodes whose Meta[MetaState] == StateArchived in the
// result. Default false: archived nodes are hidden from ordinary Search/Recall.
// The curator sweep sets it true so it can still reach (and reactivate) them.
IncludeArchived bool
```

Reactivation falls out for free: when a node is re-observed its `lastSeen` is
bumped to now (§2), so its age is small and `NextState` returns `StateActive`.

### 2. obsidian — lastSeen stamp + archived exclusion (v0.2.7, additive)

**Stamp `lastSeen` in `writeNode`**, mirroring `capturedAt`'s "honor a
caller-supplied value" rule but *without* the preserve-on-upsert reload:

- If the incoming node already carries `Meta[contracts.MetaLastSeen]`, keep it.
- Otherwise set it to `m.now().UTC().Format(time.RFC3339)`.

Consequences:
- Ordinary observation writes (Learner/`RecordShared`/`RecordPrivate` never set
  `lastSeen`) → `lastSeen` is bumped to now on every write. Re-observation
  refreshes the node.
- The curator sweep re-supplies the node's existing `lastSeen`, so a
  state-only write preserves it (no accidental reactivation).
- `capturedAt` semantics are unchanged.

**Exclude archived from reads:**
- `matchesQuery`: return false for a node whose `Meta[contracts.MetaState] ==
  contracts.StateArchived` unless `q.IncludeArchived` is true.
- `Recall`: skip archived nodes when expanding neighbors (do not add them to
  `sg.Nodes`, do not traverse through them). The explicitly-requested **root is
  always returned** even if archived — direct key access must keep working so
  G4's restore can read it; only graph expansion hides archived neighbors.

### 3. orchestrator — the sweep (herrscher-orchestrator)

`Curator` gains:

```go
now          func() time.Time // injectable clock; defaults to time.Now
staleAfter   time.Duration
archiveAfter time.Duration
```

`NewCurator`/constructors default `now` to `time.Now`. A setter
`SetStaleness(staleAfter, archiveAfter time.Duration)` configures the windows
(host wires it from env, §4).

New method:

```go
// Sweep re-derives every node's lifecycle state from its lastSeen timestamp and
// persists any change. It is deterministic (clock injected) and best-effort:
// callers on the turn path must not fail a turn if Sweep errors.
func (c *Curator) Sweep(ctx context.Context) error
```

Behavior:
1. `nodes, err := c.mem.Search(ctx, contracts.Query{IncludeArchived: true})` —
   enumerate everything, archived included.
2. For each node: read `lastSeen` from `Meta[MetaLastSeen]` (RFC3339); if absent
   or unparseable, fall back to `Meta["capturedAt"]`; if that is also absent,
   skip the node (nothing to age from).
3. `next := contracts.NextState(lastSeen, c.now().UTC(), c.staleAfter, c.archiveAfter)`.
4. **Write only on change**: if `next` differs from the current
   `Meta[MetaState]` (absent == `StateActive`), set `Meta[MetaState] = next`,
   re-supply the existing `lastSeen` (and `capturedAt`) in Meta, and
   `c.mem.Record(ctx, node)`. Unchanged nodes are not rewritten (no churn, no
   timestamp disturbance).

`Learner.Consolidate` calls `c.Sweep(ctx)` at the end of its pass, best-effort:
a sweep error is logged/swallowed, never returned, so consolidation and the
turn loop are unaffected (invariant: learning never breaks a turn).

### 4. env configuration (orchestrator plugin `register.go`)

Env is wired **inside the orchestrator plugin**, not host code — mirroring how
the obsidian plugin reads `OBSIDIAN_NODE_BUDGET` in its own `register.go`. The
host builds the local orchestrator through the plugin factory at
`core/host/seed.go:298`, where `contracts.Resolve(plugin.Manifest.Config,
os.Getenv)` already resolves every manifest-declared `Setting.Env` into the
`cfg` bag the factory receives. So G3 only:

1. Adds two `Setting`s to the orchestrator `Manifest.Config`:
   - `{Key: "stale-days", Env: "AGENT_STALE_DAYS", Help: "..."}`
   - `{Key: "archive-days", Env: "AGENT_ARCHIVE_DAYS", Help: "..."}`
2. In the `Orchestrator` factory, parses `cfg.Get("stale-days")` /
   `cfg.Get("archive-days")` (integer days → `time.Duration`) and calls
   `SetStaleness(staleAfter, archiveAfter)` on the returned `*Curator` (both
   the `NewLearner` and `NewScoped` paths return one).

Unset/unparseable → keep the built-in defaults **30 days / 90 days** (set in
the `Curator` constructor). A value `<= 0` disables that transition (nodes
never reach that state). **The host itself needs no code change** — only the
`go.mod` version bump for the new orchestrator/contracts/obsidian tags.

## Testing

**contracts (`NextState`):** table test over age buckets — fresh → active;
just past `staleAfter` → stale; just past `archiveAfter` → archived; exactly at
each threshold (`age == staleAfter`) → the higher state (`>=`); `staleAfter<=0`
→ never stale; `archiveAfter<=0` → never archived; a re-observed (recent
`lastSeen`) node that was archived → active (reactivation).

**obsidian (`writeNode` lastSeen):** a Record with no `lastSeen` → file gets
`lastSeen` == injected clock; a second Record (no lastSeen) at a later clock →
`lastSeen` bumped (unlike `capturedAt`, which is preserved — assert both in one
test); a Record that *supplies* `lastSeen` → honored verbatim.

**obsidian (archived exclusion):** `Search` with a node marked archived → node
absent by default, present when `Query{IncludeArchived:true}`; `Recall` from an
active root linked to an archived neighbor → neighbor absent from `sg.Nodes`;
`Recall` of an archived key directly → root still returned.

**orchestrator (`Sweep`):** seed nodes with controlled `lastSeen` via a mock
clock; run `Sweep`; assert states transition active→stale→archived at the
window boundaries; assert an unchanged node is not rewritten (e.g. its
`capturedAt`/`lastSeen` untouched, or via a counting fake Memory); assert
re-observing a stale node (bump `lastSeen`) then `Sweep` returns it to active;
assert `Sweep` never returns an error path that would propagate out of
`Consolidate`.

## Release footprint

- `herrscher-contracts` → **v0.2.9** (adds `state.go`,
  `Query.IncludeArchived`; additive). Tag + push, then bump downstream.
- `herrscher-obsidian-memory` → **v0.2.7** (lastSeen stamp + archived
  exclusion). Depends on contracts v0.2.9; tag after contracts.
- `herrscher-orchestrator` → new minor (Sweep + clock + config). Depends on
  contracts v0.2.9.
- host `go.mod`: bump contracts v0.2.8→v0.2.9, obsidian v0.2.6→v0.2.7,
  orchestrator to the new tag; `GOWORK=off` build + full test.

All modules imported by the public host must be public repos or master CI goes
red — tag and push in dependency order (contracts → obsidian/orchestrator →
host).

## Out of scope (YAGNI)

- The `reviewed` field: deferred to G4, whose `REPORT.md` is its first
  consumer. Stamping it every sweep would force a file write per node per sweep
  for no current reader.
- Moving archived nodes to `archive/`, the per-run `REPORT.md`, and a `restore`
  verb — all **G4**.
- Hiding archived nodes from anything other than `Search`/`Recall` (e.g. the
  ranker) — not needed; archived nodes are already excluded upstream.
- Any inactivity/scheduled trigger for `Sweep` beyond the per-consolidation
  call — that is **G5**.

## Invariants (from the umbrella roadmap)

1. **Ports only, policy not engine** — staleness is policy: a pure function in
   `contracts` plus a sweep that drives the existing `Memory` port. No new
   storage engine.
2. **Learning never breaks a turn** — `Sweep` is best-effort and off the
   turn's critical path; `Consolidate` never fails because of it.
3. **Reversible over destructive** — G3 only labels; nothing is moved or
   deleted. Archived nodes remain on disk and reactivate on re-observation.
