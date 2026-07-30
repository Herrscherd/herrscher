# ★ — Cross-agent skill promotion — design

**Date:** 2026-07-30
**Status:** Design (approved 2026-07-30)
**Slice:** ★ of the Hermes-parity + beyond-Hermes roadmap
(`docs/superpowers/specs/2026-07-30-memory-learning-hermes-parity-design.md`,
umbrella doc `2026-07-30-*-cross-agent-promotion-roadmap*`)
**Repos:** `herrscher-orchestrator` only (scope policy). No `herrscher-contracts`,
no `herrscher-obsidian-memory` change.

## Goal

The curator sees the whole shared graph: every agent's private skills
(`agents/<a>`) plus the project's shared facts (`projects/<p>`). When a private
skill has proven itself — re-observed over time, never gone stale — it is worth
every peer agent inheriting it, not just the agent that learned it. ★ adds a
**promotion** pass: it copies a proven private node into the shared project
scope so `contracts.RecallScoped` surfaces it to every agent of the project on
their next `Context`. Hermes is flat and mono-agent; it has no private/shared
split and therefore no analogous move. This is the differentiator the roadmap
calls "beyond Hermes."

★ is pure **policy**: no new heuristic seam, no LLM. Eligibility is a
deterministic, testable rule over `Meta` fields the graph already carries
(`state`, `capturedAt`, `lastSeen`) — see Design §1 for why a `Promoter` seam is
rejected.

## Baseline (verified in code, 2026-07-30)

- `Learner` (`learner.go`) already runs, at the end of `Consolidate`, best-effort
  `_ = l.Sweep(ctx)` then `_ = l.Merge(ctx)`. ★ adds a third best-effort call,
  `_ = l.Promote(ctx)`, immediately after `Merge` — Promote must see the
  post-sweep, post-merge state (freshly-archived merge originals must not be
  promoted; see §5).
- `contracts.MemoryScope{Project, Agent}` and `ProjectKey`/`AgentKey` (in
  `herrscher-contracts/*`, not re-read here but used throughout `orchestrator.go`)
  give the two key prefixes: private nodes live under `agents/<agent>/...`,
  shared nodes under `projects/<project>/...`. `contracts.RecordShared` and
  `contracts.RecordPrivate` (used today in `learner.go`'s `record` method) are
  the existing helpers that write to each scope; ★ reuses `RecordShared`
  directly — no new contracts function.
- `contracts.Node.Meta` is a flat `map[string]string`; `MetaState` /
  `MetaLastSeen` (`herrscher-contracts/state.go`) plus the sweep-only
  `Meta["capturedAt"]` convention (read in `sweep.go` as the fallback age basis
  when `lastSeen` is absent) are the only lifecycle stamps that exist today.
  **There is no re-observation counter anywhere in the codebase.** Nothing
  increments a "seen N times" field; `MetaLastSeen` is simply overwritten on
  every `Record` (per the comment in `merge.go`'s `applyUmbrella`: "obsidian's
  per-write lastSeen stamp"). So "proven" cannot mean a count; it must be built
  from the two timestamps already present.
- `Curator.Sweep` (`sweep.go`) derives `active`/`stale`/`archived` purely from
  `now - lastSeen` via `contracts.NextState`, and already skips nodes with
  `Meta[MetaMergedInto] != ""` (G2's terminal marker). `Search` without
  `IncludeArchived` (used by `Merge`, and by ★'s `Promote`) already excludes
  archived nodes.
- Config pattern (G1 `merge-min-nodes`, G2 `merge-target`/`merge-max`): a
  `contracts.Setting{Key, Env, Help}` in `register.go`'s `Manifest.Config`, read
  in the `Orchestrator` factory's Learner branch via `cfg.Get(...)`, pushed
  through a setter on `*Learner`. Default off (`0` disables).
- `Learner` composes `*Curator`, so it has `l.scope contracts.MemoryScope` and
  `l.mem contracts.Memory` available; no new fields are needed to know "my own"
  agent/project keys — `l.scope.Agent`/`l.scope.Project` are exactly that.

## Design

### 1. Deterministic rule, not a `Promoter` seam

G1/G2's `Extractor`→`Consolidator`→`Merger` chain is an **optional seam**
because *what to extract* and *how to phrase a merge* are open-ended judgment
calls that benefit from a closed LLM pass. Promotion is different: the roadmap
calls it "scope policy," and the eligibility test below is a closed-form
predicate over three `Meta` fields already on the node — no judgment, no
phrasing, no synthesis (the promoted copy is the *same* node, not a rewrite).
Introducing a `Promoter` seam here would:
- add an optional-interface no-op path for a decision that has no ambiguity to
  delegate, unlike umbrella phrasing or extraction triage;
- let a closed implementation silently promote nodes that don't meet the
  re-observation bar, undermining the one property (idempotent, inspectable
  promotion) that must hold for peer agents to trust a shared fact came from a
  proven skill.

**Decision: a deterministic rule, config-gated, no seam.** If a future need
arises for smarter triage (e.g. "only promote skills relevant to peer agents'
domains"), that composes as a *filter* on top of this rule — not a reason to
make the base mechanism heuristic.

### 2. Eligibility rule

A private node `n` (`Key` under `agents/<agent>/...`, i.e. within `l.scope.Agent`'s
subtree) is **eligible for promotion** iff all of:

1. `n.Meta[MetaState]` is `""` (implicit active) or `contracts.StateActive` —
   never stale, never archived. A node that has ever gone stale has, by
   definition, not stayed continuously useful; it is excluded even if later
   re-observed and swept back to active (age-based `NextState` is
   hysteresis-free per its doc comment, so a currently-active node may have
   been stale in between — see §6 for why this is accepted as a deliberate
   simplification, not a gap).
2. `n.Meta[MetaMergedInto] == ""` — a merged-away fragment is terminal (G2);
   never promote a folded original.
3. `n.Meta["capturedAt"]` and `n.Meta[contracts.MetaLastSeen]` are both present
   and RFC3339-parseable (the same stamps `Sweep` already relies on).
4. `lastSeen.Sub(capturedAt) >= l.promoteMinAge` — the node has been
   *re-observed* (its `lastSeen` advanced past its original capture) by at
   least the configured age window. `lastSeen` is bumped on every `Record` of
   the node (the mechanism G2 explicitly notes), so a private skill that keeps
   getting written/re-affirmed accrues age between the two stamps; a one-off
   candidate written once has `lastSeen == capturedAt` (age 0) and never
   qualifies no matter how long it then sits idle. `promoteMinAge <= 0`
   disables promotion entirely (default).
5. `n.Meta[MetaPromotedTo] == ""` — not already promoted (idempotence, §4).

This needs **no new Meta field**: `capturedAt` and `lastSeen` already exist:
```go
func (l *Learner) promoteEligible(n contracts.Node, now time.Time) bool {
    state := n.Meta[contracts.MetaState]
    if state != "" && state != contracts.StateActive {
        return false
    }
    if n.Meta[MetaMergedInto] != "" || n.Meta[MetaPromotedTo] != "" {
        return false
    }
    capturedAt, err1 := time.Parse(time.RFC3339, n.Meta["capturedAt"])
    lastSeen, err2 := time.Parse(time.RFC3339, n.Meta[contracts.MetaLastSeen])
    if err1 != nil || err2 != nil {
        return false // no reliable age basis
    }
    return lastSeen.Sub(capturedAt) >= l.promoteMinAge
}
```
(`now` is accepted for signature symmetry with `Sweep`/`NextState` but unused
by the predicate itself — age is relative between the two stamps, not to wall
clock — kept as a parameter so a future refinement, e.g. "also require
`now-lastSeen` recency," has no call-site churn.)

### 3. The `promotedTo` label (orchestrator-internal, mirrors `mergedInto`)

```go
// MetaPromotedTo, when set on a private node, names the Key of the shared copy
// it was promoted to. It is a terminal marker on the ORIGINAL: the original is
// kept (reversible, still private, still usable by its own agent) but is never
// re-promoted. Orchestrator-internal — obsidian stores Meta generically, so no
// contracts change is needed.
const MetaPromotedTo = "promotedTo"
```

Unlike `MetaMergedInto`, this does **not** archive or hide the original — the
private skill stays fully live for its own agent; only the shared *copy* is
new. This is a deliberate reversibility choice (§ Design decision below).

### 4. `Promote(ctx)` — best-effort, on `*Learner`

```go
// Promote copies each eligible private skill of this agent's own scope into the
// shared project scope, so peer agents inherit it via RecallScoped. It is
// best-effort and idempotent: disabled (promoteMinAge<=0), no project scope, or
// nil Memory all yield a clean no-op; a per-node write failure is recorded but
// never aborts the rest.
func (l *Learner) Promote(ctx context.Context) error {
    if l.promoteMinAge <= 0 || l.mem == nil || l.scope.Project == "" || l.scope.Agent == "" {
        return nil
    }
    nodes, err := l.mem.Search(ctx, contracts.Query{}) // active+stale, never archived
    if err != nil {
        return err
    }
    prefix := string(l.scope.Agent) + "/"
    now := l.now().UTC()
    var firstErr error
    for _, n := range nodes {
        if !strings.HasPrefix(n.Key, prefix) {
            continue // only this agent's own private subtree
        }
        if !l.promoteEligible(n, now) {
            continue
        }
        if err := l.applyPromotion(ctx, n); err != nil && firstErr == nil {
            firstErr = err
        }
    }
    return firstErr
}
```

`applyPromotion` writes the shared copy, links original→copy, and labels the
original — mirroring `applyUmbrella`'s write-then-label order so a crash
between the two leaves the *original* still eligible for a retry (safe
re-attempt) rather than a labeled-but-copyless original:

```go
// applyPromotion writes a shared copy of a proven private node n under the
// project scope, links the original to it, and labels the original so it is
// never re-promoted. Reversible: the original is kept, untouched apart from
// the label; nothing is archived or deleted.
func (l *Learner) applyPromotion(ctx context.Context, n contracts.Node) error {
    copy := n
    copy.Key = promotedKey(l.scope.Project, n.Key)
    copy.Meta = cloneMeta(n.Meta)
    delete(copy.Meta, MetaPromotedTo) // the copy is not itself "promoted"
    copy.Meta["promotedFrom"] = n.Key
    if err := contracts.RecordShared(ctx, l.mem, l.scope, copy); err != nil {
        return err
    }
    if err := l.mem.Links(ctx, n.Key, copy.Key, "promoted-to"); err != nil {
        return err // label not yet set: a retry re-attempts the whole write (idempotent — Record upserts by Key)
    }
    if n.Meta == nil {
        n.Meta = map[string]string{}
    }
    n.Meta[MetaPromotedTo] = copy.Key
    // Re-supply lastSeen so this state-only write does not reset the age this
    // very rule depends on (same discipline as Sweep/applyUmbrella).
    return l.mem.Record(ctx, n)
}
```

`promotedKey` derives a stable, collision-checked shared key from the
project key and the tail of the private key (e.g.
`agents/roblox-dev/skills/retry-http` →
`projects/neublox/skills/retry-http`), so re-running `Promote` after the
label failed to persist (a crash between the two `Record`s) targets the same
shared `Key` and upserts rather than duplicates:

```go
func promotedKey(project contracts.ProjectKey, agentKey string) string {
    _, tail, _ := strings.Cut(agentKey, "/") // "agents/<agent>/<tail>" -> "<tail>"
    return string(project) + "/" + tail
}
```

### 5. Reversibility decision: original stays private, not archived

The original is **kept, live, private** — only labeled. Rationale:
- The owning agent still needs and uses the skill every turn (`Context` recalls
  its own `agents/<agent>` subtree); archiving it would regress that agent's
  own continuity to promote a peer benefit, violating invariant 3 in spirit
  (never destroy value to add value elsewhere).
- Unlike a merge (G2), promotion has no "this fragment is now redundant"
  claim — the private original and the shared copy serve different audiences
  (one agent vs. all agents) and may legitimately diverge later (Merge/Sweep
  treat them as independent nodes from that point).
- `MetaPromotedTo` is purely a **do-not-repeat** marker, not a lifecycle
  state; it does not interact with `NextState`/`Sweep`'s active/stale/archived
  machine at all.

### 6. Interaction with G2 (merge) and G3 (sweep)

- **Promote runs after Sweep and after Merge** (`Consolidate`'s tail becomes
  `Sweep → Merge → Promote`), so:
  - a node merged away this pass (`MetaMergedInto` set, archived) is excluded
    from `Promote`'s `Search` result already (archived nodes are hidden) —
    belt-and-braces, `promoteEligible` also checks `MetaMergedInto` (§2.2).
  - a node newly promoted this pass is written with `Record`, which stamps a
    fresh `lastSeen` (see `merge.go`'s comment on the same obsidian behaviour);
    that is fine — the *original's* `lastSeen` is explicitly re-supplied
    (§4) so its state and age basis for a future `Merge`/`Sweep` pass are
    undisturbed.
- **Sweep must not need any change**: `Sweep` walks `IncludeArchived: true`
  and only inspects `MetaState`/`MetaLastSeen`/`MetaMergedInto`; a node
  carrying `MetaPromotedTo` is simply a normal node to it (state transitions
  proceed exactly as before promotion — if the owning agent stops re-using it,
  it goes stale/archived independent of having been promoted).
- **Merge must not re-fold a promoted original together with its shared
  copy**: they are different `Key`s in different scope subtrees; `Merge`
  groups by `Meta["domain"]` across the whole graph (not by scope), so a
  private original and its shared copy *could* otherwise land in the same
  domain group and be folded by a sufficiently permissive `merge-target=all`.
  This is resolved, not accepted as a risk — see "### Interaction with G2
  merge" below.
- **The shared copy is a normal `projects/` fact from that point on** — no
  special-case in `RecallScoped`, no new Meta beyond the informational
  `promotedFrom`.

### Interaction with G2 merge

Merge's candidate filter must skip nodes bearing `MetaPromotedTo` (the
promoted original), exactly as it skips `MetaMergedInto`, so a promoted
original and its shared copy are never re-fused. Concretely, G2's
`mergeEligible` in `merge.go` gains the same guard as its existing
`MetaMergedInto` skip:

```go
func (l *Learner) mergeEligible(n contracts.Node) bool {
    if n.Meta[MetaMergedInto] != "" || n.Meta[MetaPromotedTo] != "" {
        return false
    }
    // ... existing state/target checks unchanged
}
```

A promoted original is not itself terminal the way a merged-away fragment
is — it stays live and recallable for its own agent — but it must never be
folded into an umbrella alongside (or instead of) its own shared copy, since
that would either duplicate the fact across scopes under a new umbrella key
or silently sever the `promoted-to` link's meaning. Excluding
`MetaPromotedTo` nodes from merge candidacy entirely (rather than only
excluding same-domain pairs with their own copy) is the simplest rule that
holds: a node that has been promoted is "settled" from merge's point of view,
just as a merged-away node is.

### 7. Config surface

Two new `Setting`s, mirroring G2's table exactly:

| Setting key | Env | Default | Meaning |
|---|---|---|---|
| `promote-min-age-days` | `MEMORY_PROMOTE_MIN_AGE_DAYS` | `0` (off) | days a private node's `lastSeen` must exceed its `capturedAt` before it is eligible for promotion; `<=0` disables ★ cleanly. |

(`promote-target` from the task brief is **not added** — see Out of scope: the
rule already scopes to `l.scope.Agent`'s own subtree; there is no second axis
to select, unlike G2's stale/active/all, because promotion by definition acts
on *this agent's* private nodes only, not an arbitrary node population.)

```go
// SetPromote configures the ★ cross-agent promotion pass. minAge <= 0 disables
// the pass (default).
func (l *Learner) SetPromote(minAge time.Duration) {
    l.promoteMinAge = minAge
}
```

`register.go` wiring, in the Learner branch, after `l.SetMerge(...)`:

```go
promoteDays, _ := strconv.Atoi(cfg.Get("promote-min-age-days"))
l.SetPromote(time.Duration(promoteDays) * 24 * time.Hour)
```

New `Manifest.Config` entry:
```go
{Key: "promote-min-age-days", Env: "MEMORY_PROMOTE_MIN_AGE_DAYS", Help: "days a private skill's lastSeen must exceed its capturedAt before the curator promotes it to the shared project scope; <=0 disables (default 0, off)", Required: false},
```

New field on `Learner` (`learner.go`): `promoteMinAge time.Duration`.

## Idempotence

- `MetaPromotedTo` on the original is the durable, sole idempotence record: once
  set, `promoteEligible` returns false for that node on every future pass —
  no `seen`-style in-memory tracking needed (same discipline as G2's
  `MetaMergedInto`).
- A crash between writing the shared copy and labeling the original is safe: a
  retry recomputes the same `copy.Key` (`promotedKey` is a pure function of the
  original's key and the project key) and `RecordShared` **upserts** by Key
  (per `contracts.Memory.Record`'s doc comment), so the retry overwrites the
  same shared node rather than duplicating it, then proceeds to label the
  original.
- Re-running `Promote` with the same config is a no-op for every
  already-promoted node; a node that later becomes newly eligible (crosses
  `promoteMinAge`) is picked up on the next `Consolidate` pass.
- `Merge`'s `mergeEligible` skipping `MetaPromotedTo` nodes (see "Interaction
  with G2 merge" above) is itself idempotent with respect to promotion: a
  promoted original can never be pulled into an umbrella on any later pass,
  so `MetaPromotedTo` remains a stable, one-way terminal marker for merge
  purposes for the lifetime of the node.

## Testing (orchestrator, fake `Memory`)

- **Disabled by default → no-op:** `promoteMinAge == 0` (or `SetPromote` never
  called); `Promote` makes no `Search`/`Record` calls.
- **No project or agent scope → no-op:** `l.scope.Project == ""` or
  `l.scope.Agent == ""`; `Promote` is a clean no-op even with `promoteMinAge` set.
- **Below age threshold → not promoted:** a private node with
  `lastSeen.Sub(capturedAt) < promoteMinAge` is skipped.
- **Happy path:** a private node meeting the age bar gets a shared copy under
  `projects/<p>/...`, a `promoted-to` link from original to copy, and
  `Meta[MetaPromotedTo]` set on the original; the original's `Meta[MetaState]`/
  `lastSeen` are unchanged apart from the new label.
- **Stale/archived excluded:** a private node with `state=stale` or
  `state=archived` is never eligible even if old enough.
- **Merged-away excluded:** a node with `Meta[MetaMergedInto]` set is skipped.
- **Idempotent re-run:** running `Promote` twice in a row promotes once; the
  second run makes zero `Record` calls for that node (spy asserts call count).
- **Scope isolation:** a private node belonging to a *different* agent
  (`agents/<other>/...`) is never scanned/promoted by this `Learner` instance
  (prefix filter).
- **Missing/unparseable timestamps → not eligible:** a node lacking
  `capturedAt` or `lastSeen`, or with an unparseable stamp, is skipped (fails
  open to "not yet provable," never panics).
- **Best-effort:** a `Record`/`Links` failure on one node's promotion is
  recorded as the first error but does not stop other eligible nodes from
  being promoted in the same pass.
- **Ordering with Consolidate:** `Consolidate` calls `Sweep`, then `Merge`,
  then `Promote`, and swallows all three errors independently (a `Promote`
  error never masks or is masked by a `Merge`/`Sweep` error — first-error
  semantics per existing `Consolidate` pattern).

## Release footprint

- `herrscher-orchestrator` → **v0.1.13** (Promote pass + `MetaPromotedTo` +
  config + `SetPromote`). Depends only on already-released contracts v0.2.9 —
  no contracts/obsidian bump needed.
- host `go.mod`: bump orchestrator v0.1.12 → v0.1.13; `GOWORK=off` build + full
  test against real tags.
- README "Learning (the write side)" gains a **Cross-agent promotion**
  paragraph; the memory-vs-hermes gap note marks ★ shipped (the "beyond Hermes"
  differentiator).

## Out of scope (YAGNI)

- **A `promote-target` axis** — unlike G2's stale/active/all, promotion has no
  second population to select; it always means "this agent's own active,
  proven, private nodes." Add only if a future need (e.g. promoting *any*
  agent's proven skills, not just the running agent's own) materializes.
- **A dedicated re-observation counter Meta field** — the `lastSeen - capturedAt`
  age gap is sufficient and reuses existing stamps; a counter is strictly more
  bookkeeping for no behavioural gain today. Revisit only if the age-gap proxy
  proves too coarse in practice (e.g. one long-lived but rarely-reinforced
  node vs. one heavily re-affirmed in a day both pass the same bar).
- **A restore/"demote" command** — the original is never destroyed, so nothing
  needs restoring; if a promoted shared copy needs retracting, that is a
  manual `Delete`/edit on the shared node, unrelated to this pass.
- **Cross-project promotion** — ★ only ever writes to `l.scope.Project`, this
  agent's own project; promoting a skill into a *different* project's shared
  scope is not in the roadmap slice and is not supported.

## Invariants (from the umbrella roadmap)

1. **Ports only, policy not engine** — ★ is a pure Go predicate plus plumbing
   over the existing `contracts.Memory`/`RecordShared`/`Links` port; no new
   `contracts.Memory` method, no new engine, no contracts or obsidian change.
2. **Learning never breaks a turn** — `Promote` is called best-effort at the
   tail of `Consolidate` (`_ = l.Promote(ctx)`), whose result is swallowed by
   `Observe`; a `Promote` error is recorded internally but never propagates to
   the turn loop, and `Sweep`/`Merge` already ran unaffected by its presence.
3. **Reversible over destructive** — the private original is never deleted,
   archived, or rewritten beyond one label (`MetaPromotedTo`) and an untouched
   `lastSeen`; the shared copy is a new node. Nothing about promotion can be
   undone destructively because nothing was destroyed to perform it.
4. **Merge/promotion never re-fuse** — G2's `mergeEligible` skips any node
   bearing `Meta[MetaPromotedTo]`, mirroring its existing `MetaMergedInto`
   skip, so a promoted original and its shared copy are never folded back
   together by a later `Merge` pass (see "Interaction with G2 merge," §6).
