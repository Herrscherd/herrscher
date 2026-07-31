# G4 — Reversible archive audit trail (REPORT node) + `memory restore` — design

**Date:** 2026-07-30
**Status:** Design (approved 2026-07-30)
**Slice:** G4 of the Hermes-parity roadmap
(`docs/superpowers/specs/2026-07-30-memory-learning-hermes-parity-design.md`)
**Repos:** `herrscher-orchestrator` (Sweep/Merge summary + Restore + report writer +
config) and the `herrscher` host (`memory restore` CLI verb). **No**
`herrscher-contracts` change, **no** `herrscher-obsidian-memory` change.

## Goal

Give every curator pass (G1 budget, G2 merge, G3 staleness) an audit trail, and
give a human/operator an explicit way to undo an archival. The roadmap's G4 text
says archiving should "move nodes to an archive/ area instead of deleting" — but
G3 (already shipped) never deleted anything: it only ever flips
`Meta[MetaState]` to `"archived"` on the node's *existing* file and lets `Search`
hide it. That already satisfies "reversible over destructive" without a storage
move. So G4 adds exactly two things on top of the existing label: **(a)** a
per-`Consolidate`-pass **REPORT** recording which keys transitioned, and **(b)**
a **restore** path that clears the label back to active. Nothing is moved,
copied, or deleted; there is no new file layout to invent.

## Baseline (verified in code, 2026-07-30)

- `sweep.go`: `Sweep` walks `Search(Query{IncludeArchived:true})`, computes
  `contracts.NextState(lastSeen, now, staleAfter, archiveAfter)`, and `Record`s a
  node only when its state changed — skipping nodes carrying `MetaMergedInto`
  (terminal). It returns only `error` (first write failure); no record of *which*
  keys changed survives the call today.
- `orchestrator.go`: `Curator` holds `mem`, `session`, `scope`, `now`,
  `staleAfter`, `archiveAfter`. `SetStaleness` is the existing config-setter
  pattern G4 will follow for report config.
- `learner.go`: `Learner.Consolidate` runs candidates, then
  `_ = l.Sweep(ctx)`, then `_ = l.Merge(ctx)` (G2), then `return firstErr` — the
  exact best-effort tail G4's report emission is appended to.
- `contracts/memory.go`: `Memory` is `Recall/Record/Search/Links/Close`, plus
  optional capabilities `Provisioner`, `Locator`, `Deleter`. There is **no**
  `Restore`/`Undelete` capability and none is needed — restoring a label-archived
  node is just a `Recall` + mutate `Meta` + `Record`, already expressible over the
  existing port.
- `contracts/state.go`: `StateActive/StateStale/StateArchived`,
  `MetaState`, `MetaLastSeen`. `NextState` is state-less (derived purely from
  age) — so simply clearing `Meta[MetaState]` is not durably "active" on its
  own if the node is still old; restore must also refresh `MetaLastSeen` (see
  §2) or the very next `Sweep` would immediately re-archive it.
- `contracts.Node.Kind` (`contracts/state.go`'s sibling `NodeKind` in
  `memory.go`) is a plain string type; **nothing in obsidian or contracts
  switches on it to gate behaviour** — `obsidian/memory.go:359` only compares
  `n.Kind == k` inside `Query.Kinds` filtering. A new orchestrator-local kind
  value therefore needs no contracts change (confirmed: G2's `MetaMergedInto`
  already sets the precedent of an orchestrator-local constant with no
  contracts/obsidian involvement).
- `herrscher-obsidian-memory/memory.go`: `Record` upserts by Key (temp+rename,
  atomic); `Recall`'s neighbour walk (`memory.go:248`) already skips archived
  children; `Search` (`memory.go:353`) already excludes `StateArchived` unless
  `Query.IncludeArchived`. **No archive/ directory or file-move concept exists
  anywhere in this repo.** Confirms: G4 needs no obsidian change.
- `herrscher` host, `core/host/cli.go:89-171`: existing `memory locate` /
  `memory forget` / `memory record` verbs share one pattern — `BuildFirstMemory
  (cmdCtx)` builds a throwaway `contracts.Memory`, `defer mem.Close()`, then
  either call a port method directly (`Record`) or type-assert an optional
  capability (`Locator`, `Deleter`) and call it. `memory forget` is the closest
  precedent for a mutating one-key verb. There is no capability type-assertion
  needed for restore since it's expressible directly over `Recall`+`Record` —
  see §3 for why it lives in `orchestrator` as a free function rather than a new
  `contracts.Memory` capability.
- `herrscher/plugins.go:19` blank-imports `herrscher-orchestrator` already;
  `herrscher/b3_consolidation_test.go` and `bridge_test.go` already import it
  non-blank (`orchestrator "github.com/Herrscherd/herrscher-orchestrator"`), so
  `core/host/cli.go` importing it directly for `orchestrator.Restore` is
  consistent with existing usage, not a new dependency edge.
- `register.go`: `contracts.Setting{Key, Env, Help}` in the `Manifest`, read via
  `cfg.Get(...)`, pushed through a setter — the pattern report config reuses.

## Design

### 1. `Transition` + a returnable summary from `Sweep` and `Merge`

```go
// Transition is one state change a curator pass made to one node, for the
// audit report. Kind is "sweep" (G3 staleness) or "merge" (G2 umbrella-fold).
type Transition struct {
    Key   string // node Key that changed
    From  string // prior Meta[MetaState] ("" treated as StateActive)
    To    string // new Meta[MetaState]
    Kind  string // "sweep" | "merge"
}
```

`Sweep` and `Merge` keep their existing signatures and error-only return (no
breaking change to callers or tests that already assert on `error`); instead
each **appends** to a slice owned by the `Curator`:

```go
// Curator gains (orchestrator.go):
transitions []Transition // this Consolidate pass's audit trail; reset each pass
```

`Sweep`, at the point it currently does `n.Meta[contracts.MetaState] = next`
(sweep.go, just before the `Record` call), appends
`Transition{Key: n.Key, From: cur, To: next, Kind: "sweep"}` — recorded
**before** the write attempt succeeds/fails is wrong (a failed write shouldn't
claim a transition happened), so the append moves to **after** a successful
`c.mem.Record`, mirroring the existing `firstErr` bookkeeping:

```go
if err := c.mem.Record(ctx, n); err != nil {
    if firstErr == nil {
        firstErr = err
    }
    continue // do not record a transition for a write that didn't land
}
c.transitions = append(c.transitions, Transition{Key: n.Key, From: cur, To: next, Kind: "sweep"})
```

`Learner.Merge`'s `applyUmbrella` gets the same treatment: on a successful
umbrella `Record` + successful original archive `Record`, append
`Transition{Key: original.Key, From: prevState, To: contracts.StateArchived, Kind: "merge"}`
per original, plus one `Transition{Key: umbrella.Node.Key, From: "", To: "active", Kind: "merge"}`
for the new node itself (so the report shows what appeared, not just what
disappeared).

### 2. `Restore` — a free function in `orchestrator`, not a new `Memory` capability

```go
// Restore reactivates an archived (or merged-into) node at key: it clears
// Meta[MetaState] to active and refreshes Meta[MetaLastSeen] to now, so the very
// next Sweep does not immediately re-derive it back to stale/archived from a
// stale timestamp. Idempotent: restoring an already-active node is a no-op write
// that still refreshes lastSeen.
//
// A node still carrying Meta[MetaMergedInto] is a folded fragment, not an
// independent archived node — restoring it without also detaching it from its
// umbrella would resurrect a fragment the merge pass deliberately subsumed, while
// Sweep's mergedInto guard (sweep.go) would immediately re-skip it, and the next
// Merge pass could re-fold it right back. Restore therefore REFUSES a merged
// original by default: it returns ErrMergedOriginal unless the caller passes
// Force(true) (below), in which case it also clears Meta[MetaMergedInto] so the
// node becomes independent again (the umbrella node itself is untouched — it may
// now have one fewer live member, which is fine; umbrellas are additive, not
// authoritative).
func Restore(ctx context.Context, mem contracts.Memory, key string, opts ...RestoreOption) error

// RestoreOption configures one Restore call. Force, when set, allows restoring a
// merged-into original by also clearing Meta[MetaMergedInto].
type RestoreOption func(*restoreConfig)
func Force(force bool) RestoreOption

var ErrMergedOriginal = errors.New("orchestrator: node is folded into an umbrella; restore with Force to detach it")
```

Implementation: `Recall(ctx, key, 0)` the node (an error, including "not
found", surfaces unchanged — restore of an absent key is a real error, unlike
`Deleter.Delete`'s intentional idempotent-on-absent contract, because there is
nothing to reactivate); if `Meta[MetaMergedInto] != ""` and `!force`, return
`ErrMergedOriginal`; otherwise set `Meta[MetaState] = contracts.StateActive`,
`Meta[contracts.MetaLastSeen] = now.UTC().Format(time.RFC3339)`, clear
`Meta[MetaMergedInto]` only when force-detaching, and `Record` it back.

This is a **free function taking `contracts.Memory`**, not a `*Curator` method
and not a new `contracts.Memory` capability interface, because:
- it needs no orchestrator state (`now`, `staleAfter`, …) beyond the injected
  clock, which every caller can supply via a plain `time.Now`-shaped param
  (`clock func() time.Time`, defaulted internally to `time.Now` — mirrors
  `Curator.now` without requiring a `Curator` instance);
- the host's `memory restore` CLI verb (§4) builds a bare `contracts.Memory`
  via `BuildFirstMemory` with **no session/scope**, the same shape as the
  existing `memory forget`/`memory record` verbs — routing it through
  `*Curator` would force those verbs to also construct a Curator for no
  behavioural gain;
- `*Learner` still gets a thin, testable wrapper for symmetry with `Sweep`/
  `Merge` and to log a `Transition{Kind: "restore"}` into the pass's audit
  trail when restore happens mid-session:
```go
// Restore reactivates key and records the transition on l.transitions
// (best-effort: a report-append failure never surfaces here).
func (l *Learner) Restore(ctx context.Context, key string, opts ...RestoreOption) error {
    err := Restore(ctx, l.mem, key, opts...)
    if err == nil {
        l.transitions = append(l.transitions, Transition{Key: key, From: contracts.StateArchived, To: contracts.StateActive, Kind: "restore"})
    }
    return err
}
```

### 3. The REPORT — a `contracts.Node`, not a raw file

Writing the report through the existing `Record` port (instead of the host
reaching into the vault's filesystem) keeps invariant 1 (ports only): the
orchestrator never learns a file path, and obsidian needs no new writer.

```go
// ReportKind is the orchestrator-local Node.Kind used for a per-pass audit
// report. No contracts change: NodeKind is a plain string type and nothing in
// contracts/obsidian switches on Kind to gate behaviour (verified: obsidian only
// compares it inside Query.Kinds filtering).
const ReportKind contracts.NodeKind = "report"

// reportPrefix roots every report under one recallable key so `memory search
// --kind report` (existing generic Search) finds them; Sweep/Merge/Restore
// content already excludes archived nodes from ordinary browsing, so reports
// living as ordinary nodes do not clutter Context/Recall (Context only ever
// recalls the session key + P1 scope roots, never an arbitrary Search).
const reportPrefix = "reports/"
```

At the end of `Consolidate`, after `Sweep` and `Merge` have both run and
populated `l.transitions`:

```go
_ = l.Sweep(ctx)
_ = l.Merge(ctx)
_ = l.report(ctx) // best-effort: a report write must never break the turn
l.transitions = nil // reset for the next pass regardless of write outcome
return firstErr
```

`report(ctx)`:
1. If `!l.reportEnabled` or `len(l.transitions) == 0` → return nil (no empty
   reports; keeps the vault free of no-op noise on a quiet pass).
2. Render a markdown body: one table row per `Transition`
   (`| key | kind | from | to |`), plus a one-line header stating the pass
   timestamp and counts per `Kind`.
3. `Record` a `Node{Key: reportPrefix + now.UTC().Format(time.RFC3339), Kind:
   ReportKind, Title: "consolidate report " + <timestamp>, Body: <table>}` — a
   **new key per run** (append-only history), not one rewritten file, because
   G4's Accept criterion is an audit trail across runs, and upsert-by-Key would
   silently overwrite yesterday's report if the key were fixed.
4. Report-write failure is folded into `firstErr` exactly like a Sweep/Merge
   failure (best-effort, never surfaces past `Consolidate`).

No rotation/pruning is implemented in G4 (see Out of scope) — one node per
`Consolidate` run, and G3's own staleness sweep will eventually mark old report
nodes stale/archived like any other node (they carry no `lastSeen` refresh
themselves, so they age out naturally under existing G3 defaults — a nice
side-effect requiring zero new code).

### 4. Host CLI: `memory restore`

Mirrors `memory forget` in `core/host/cli.go`:

```go
if err := reg.Add(contracts.New("memory", "restore").
    Help("reactivate an archived (or merged) memory node by key").
    Param("key", "node key", true).
    ValueParam("force", "detach from its umbrella if the node was merged (true/false)", false).
    Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
        mem, err := BuildFirstMemory(cmdCtx)
        if err != nil {
            return "", err
        }
        defer mem.Close()
        force := in.Get("force") == "true"
        if err := orchestrator.Restore(cmdCtx, mem, in.Get("key"), orchestrator.Force(force)); err != nil {
            return "", err
        }
        return "restored " + in.Get("key"), nil
    })); err != nil {
    return nil, hostDeps{}, err
}
```

Needs one new import in `core/host/cli.go`:
`orchestrator "github.com/Herrscherd/herrscher-orchestrator"` (already a
project dependency; see baseline).

### 5. Config surface

Report emission follows the existing flag+env+settings triple used for
staleness/merge, defaulting **on** (an audit trail with no cost beyond a
best-effort node write per pass, matching "adds the audit trail Hermes has"):

| Setting key | Env | Default | Meaning |
|---|---|---|---|
| `report-enabled` | `MEMORY_REPORT_ENABLED` | `true` | write a REPORT node at the end of a `Consolidate` pass that made ≥1 transition. `false`/`0`/`off` disables. |
| `report-prefix` | `MEMORY_REPORT_PREFIX` | `reports/` | key prefix each report node is written under (timestamp appended). Must be non-empty; invalid falls back to default. |

```go
// SetReport configures G4 report emission. enabled=false is a clean no-op;
// prefix="" falls back to the default "reports/".
func (l *Learner) SetReport(enabled bool, prefix string)
```

Wired in `register.go`'s Learner branch alongside `SetStaleness`/`SetMerge`:

```go
reportEnabled := cfg.Get("report-enabled") != "false" && cfg.Get("report-enabled") != "0" && cfg.Get("report-enabled") != "off"
l.SetReport(reportEnabled, cfg.Get("report-prefix"))
```

(`report-enabled` is absent by default → the three negative-string checks all
fail → `true`, matching the documented default without a separate "was it
set" bookkeeping.)

## Idempotence

- `Sweep`/`Merge` transitions are pass-scoped (`l.transitions` reset to `nil`
  unconditionally at the end of `Consolidate`, win or lose) — a report never
  accumulates duplicate rows across passes.
- Each report is a freshly-keyed node (`reportPrefix + timestamp`); re-running
  `Consolidate` twice at different times produces two distinct report nodes,
  never a duplicate-suppressed no-op — that IS the desired behaviour (an
  append-only audit log), unlike G1/G2's dedup-by-Key upserts.
- `Restore` is idempotent on an already-active node (clears a no-op `Meta
  [MetaState]`, refreshes `lastSeen`, writes) and a well-defined error
  (`ErrMergedOriginal`) rather than a silent no-op on a merged original absent
  `Force` — restore is a deliberate, auditable action, not swept under
  best-effort.
- Restoring twice with `Force` is safe: the second call finds
  `Meta[MetaMergedInto] == ""` already and takes the plain-restore path.

## Testing (orchestrator, fake `Memory`)

- **Sweep populates transitions:** a stale→archived node produces exactly one
  `Transition{Kind:"sweep"}`; an unchanged node produces none.
- **Sweep write failure produces no transition:** a fake `Memory.Record` error
  on one node means that node contributes no `Transition` even though `NextState`
  computed a change; `firstErr` is still set.
- **Merge populates two transitions per fold:** one umbrella `Transition{To:
  "active"}` plus one per original `Transition{To: "archived", Kind:"merge"}`.
- **Report skipped when no transitions:** a quiet `Consolidate` pass (no sweep
  or merge activity) writes zero report nodes.
- **Report written with the right shape:** ≥1 transition → exactly one `Record`
  with `Kind == ReportKind`, `Key` starting with the configured prefix, body
  containing every transitioned key.
- **Report disabled → no write:** `SetReport(false, "")` then a pass with
  transitions writes no report node.
- **Restore happy path:** an archived node (no `mergedInto`) → `Meta[MetaState]
  == active`, `Meta[MetaLastSeen]` refreshed to the injected clock's `now`.
- **Restore refuses a merged original:** a node with `Meta[MetaMergedInto] !=
  ""` and no `Force` → `ErrMergedOriginal`, node left untouched (still archived,
  still linked to its umbrella).
- **Restore with Force detaches:** same node + `Force(true)` → active,
  `Meta[MetaMergedInto]` cleared; the umbrella node itself is not rewritten.
- **Restore of an absent key errors:** unlike `Deleter.Delete`, a missing key is
  a real error (nothing to reactivate), not a silent success.
- **`Learner.Restore` appends a transition:** a successful restore call adds
  exactly one `Transition{Kind:"restore"}` to the pass's audit trail (surfaced
  in the next report if `Consolidate` runs before the field resets — or,
  simpler: assert directly on `l.transitions` post-call, since `Restore` is
  typically called out of band between passes).
- **Host CLI (`herrscher`, mirroring `memory_verbs_test.go`):** `memory
  restore --key <archived key>` reactivates it; `--force=true` detaches a
  merged original; restoring an unknown key errors.

## Release footprint

- `herrscher-orchestrator` → **v0.1.13** (`Transition` type, `Sweep`/`Merge`
  transition bookkeeping on `*Curator`/`*Learner`, `Restore` free function +
  `Force` option + `ErrMergedOriginal`, `Learner.Restore` wrapper, `report`
  method + `ReportKind` + `SetReport` + config triple). Depends only on the
  already-released `contracts` v0.2.9 — no contracts bump.
- `herrscher` host: bump orchestrator v0.1.12 → v0.1.13; `core/host/cli.go`
  gains the `memory restore` verb + a non-blank `orchestrator` import;
  `register.go` (orchestrator repo) gains `report-enabled`/`report-prefix`
  settings. `GOWORK=off` build + full test against real tags.
- No `herrscher-obsidian-memory` release: no code or behaviour there changes.
- README "Learning (the write side)" gains a **Reversible archive + audit
  report** paragraph; the memory-vs-hermes note updates G4 from "herrscher
  deletes" (stale — G3 already made it label-based) to "shipped: report +
  restore, no delete, no contracts/obsidian change."

## Out of scope (YAGNI)

- **A physical `archive/` directory / file move in obsidian.** The roadmap
  text names it, but G3 already achieves reversibility via a label, and
  obsidian's `Search`/`Recall` already hide archived nodes from ordinary
  traffic (verified: `memory.go:248`, `:353`). Moving files would add a real
  storage-engine change (path remapping, migration of existing vaults) for zero
  behavioural gain over the label, and would violate invariant 1 (ports only,
  policy not engine) by teaching the orchestrator about vault layout. If a
  human later wants archived notes visually grouped in Obsidian's file
  explorer, that is an **obsidian-only, additive** follow-up (e.g. a vault
  option to symlink/mirror archived notes under an `archive/` view) — not
  needed to satisfy G4's Accept criteria.
- **Report rotation / pruning / max-report-count.** Reports are ordinary
  nodes; G3's existing staleness sweep already ages them out over time with no
  new code. A dedicated retention policy can be added later if the report
  volume becomes a real problem.
- **A single rewritten `REPORT.md` file instead of per-run nodes.** Rejected:
  it would need upsert-by-fixed-Key (overwriting history) or in-orchestrator
  file-append logic bypassing the `Memory` port. Per-run nodes keep the port
  boundary clean and give a natural query surface (`Search{Kinds:
  [ReportKind]}`).
- **A `memory report` CLI verb to print the latest report.** The existing
  generic `Search`/`Recall` verbs (not shown here, out of this repo) already
  reach report nodes by key/kind; a dedicated verb is a thin convenience layer,
  not required by G4's Accept criteria, and can be added independently.
- **Restoring a stale (not yet archived) node.** `Restore` targets archived
  (and merged-into) nodes; a merely-stale node is still live/recallable and
  needs no restore path — re-observing it (a fresh `lastSeen`) already reverts
  it to active via the next `Sweep`.

## Invariants (from the umbrella roadmap)

1. **Ports only, policy not engine** — G4 adds a `Transition`/report type and a
   `Restore` free function over the existing `contracts.Memory` port
   (`Recall`+`Record`); no new `contracts.Memory` capability interface, no
   `contracts`/obsidian change, no file-layout knowledge in the orchestrator.
2. **Learning never breaks a turn** — the report write is appended to
   `Consolidate`'s existing best-effort tail (`_ = l.report(ctx)`), folded into
   `firstErr` exactly like `Sweep`/`Merge`; `Restore` itself is an explicit,
   out-of-band operator action (CLI verb or direct call), not on the turn path,
   so it is allowed to return a real error to its caller.
3. **Reversible over destructive** — G4 does not touch G3's label-based
   reversibility; it adds visibility (the report) and an explicit undo
   (`Restore`) on top of it. `Restore` itself defaults to refusing to silently
   resurrect a folded merge fragment (`ErrMergedOriginal`), requiring an
   explicit `Force` — reversal is available but never accidental.
