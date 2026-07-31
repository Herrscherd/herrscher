# Memory `Unlink` primitive — force-restore stale-edge debt (G4 follow-up)

**Date:** 2026-07-31
**Status:** design (awaiting user review)
**Scope:** contracts v0.2.12 → obsidian v0.2.10 → orchestrator v0.1.20 → host branch `herrscher-memory-review`

## Problem

G4 shipped reversible archive + restore. `Restore(..., Force)` (orchestrator
`restore.go`) reactivates a merged original by clearing `Meta[MetaMergedInto]`,
but it **cannot remove the `merged-into` edge** the merge created
(`merge.go:192` → `mem.Links(k, umbrella, "merged-into")`). The `contracts.Memory`
port has `Links` but no inverse, so a force-restored node keeps a dangling
`merged-into` wikilink pointing at its old umbrella. This was the one deferred
defect flagged in the G4 finalization review.

## Why a naive removal fails

In the obsidian impl, edges are **literal `[[to|rel]]` text in the node body**.
`marshalNode` (vault.go) only *appends* wikilinks that are not already present in
the body; it never removes one. `unmarshalNode` re-derives `n.Links` by scanning
the body with `wikilinkRe`. So dropping the entry from `n.Links` and re-recording
is a no-op — marshalNode sees the `[[to]]` still in the body and the edge
survives a round-trip. **Unlink must excise the wikilink from the body itself.**

## Design

Identity is **by pair `(from, to)`**, no `rel` — mirroring `Links`, which dedups
on `to` alone. Removing an edge to `to` removes it regardless of relation label.

### Volet 1 — contracts (`memory.go`), v0.2.12

Add one method to the `Memory` port, mirroring `Links`:

```go
// Unlink removes the typed edge from→to (every relation targeting `to`),
// the inverse of Links. Idempotent: an absent edge is not an error.
Unlink(ctx context.Context, from, to string) error
```

This extends the core port (not an optional capability like `Deleter`): every
implementation must provide it. Consequence — the obsidian impl plus every test
fake in host and orchestrator that satisfies `contracts.Memory` gains an `Unlink`
method. Doc-comment on `Links` updated to name its inverse.

### Volet 2 — obsidian (`memory.go`), v0.2.10

`Unlink(ctx, from, to)`:

1. Under `m.mu` + `m.flock`, `loadUnlocked(from)`.
2. Drop from `n.Links` every link whose `To == to`.
3. **Excise from `n.Body`** every wikilink targeting `to`:
   - a managed bullet line `- [[to]]` / `- [[to|rel]]` (optionally trailing
     whitespace) → remove the whole line;
   - an inline `[[to]]` / `[[to|rel]]` token → remove just the token, then
     collapse a resulting double space / space-before-punctuation left behind.
   - Matching keys the wikilink target (`m[1]`) against `to` exactly.
4. `recordUnlockedNoReload(n)`.

A now-empty `## Liens` section (header with no remaining bullets) is left as-is —
harmless, and marshalNode's `liensHeaderRe` guard already avoids emitting a
second header. Idempotent: `to` absent → load, no change, record (or short-circuit
before writing — either is acceptable; short-circuit avoids a needless mtime bump).

Regression test: link `a→b`, `Unlink(a,b)` → `b` absent from `a`'s `Links` and
from a `Recall(a)` subgraph; a sibling human wikilink and surrounding prose in
`a`'s body are byte-identical afterward.

### Volet 3 — orchestrator (`restore.go`), v0.1.20

In the `Force` branch, after clearing `MetaMergedInto`, call
`mem.Unlink(ctx, node.Key, umbrella)` to drop the residual `merged-into` edge.
The umbrella key is the value read from `Meta[MetaMergedInto]` before clearing it.
Best-effort: an `Unlink` error is logged/discarded, not propagated — consistent
with "learning never breaks the turn" and with the fact that the state flip
(the user-visible reactivation) already succeeded. The `restore` `Transition`
audit record is unchanged.

Regression test: merge two nodes → force-`Restore` one → assert the `merged-into`
edge is gone from the graph (Search/Recall no longer traverses original→umbrella).

### Volet 4 — host (`cli.go`)

Add `memory unlink --from K1 --to K2`, symmetric to `memory restore`: resolves the
first memory plugin (`BuildFirstMemory`), calls `mem.Unlink(ctx, from, to)`, prints
a one-line confirmation. Both flags required; missing → usage error. README
memory section gains a line for the verb.

## Versions & release

contracts v0.2.12 → obsidian v0.2.10 (go.mod bump to contracts v0.2.12) →
orchestrator v0.1.20 (go.mod bump to contracts v0.2.12) → host go.mod bumps all
three. Release parity verified `GOWORK=off`; tags pushed in dependency order.
All git/tag/network ops are the main agent's; subagents commit locally only.

## Invariants

1. **Ports-only** — behaviour lives in the impl; the port gains one neutral verb.
2. **Learning never breaks the turn** — force-restore Unlink is best-effort.
3. **Surgical** — only wikilinks targeting `to` are touched; human prose and
   other edges are byte-preserved (regression-locked).
4. **Idempotent** — Unlink of an absent edge is a no-op, not an error.
