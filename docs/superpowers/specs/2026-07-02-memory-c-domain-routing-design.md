# Memory C — Domain routing layer

**Date:** 2026-07-02
**Status:** Design (approved for spec)
**PR order:** 1 of 4 (C → B → A → D)
**Repos touched:** `herrscher-contracts`, `herrscher-obsidian-memory`

## Context

herrscher's memory is already a hierarchical knowledge graph, not a flat store.
The structural spine is `Organization → Project → Repo/Server` with documentary
kinds (`Architecture/Production/Session/Decision/User/Agent`), defined in
`contracts/memory.go` (`NodeKind`). The Obsidian plugin scaffolds
`<org>/<project>/{index,architecture,production,repos/*,servers/*}` in
`scaffold.go` (`Init`).

The "Vault Obsidian / Memory" diagram (Routing/Index → Dev/Research/Content/
Investment vaults) is a *domain* routing layer that sits **above** projects: a
transverse grouping of knowledge by area of concern, independent of which
project a fact belongs to. herrscher has no such concept today — the top of the
spine is `Organization`, which is an ownership boundary, not a topical one.

## Goal

Add a lightweight **domain** layer so that memory can be routed and recalled by
area of concern (e.g. `dev`, `research`), matching the diagram's per-vault
routing, without disturbing the existing `Org→Project` spine.

## Non-goals

- No embeddings / semantic routing (that is chantier A).
- No per-domain physical vault split — one vault, domain is a graph node + tag.
- Not prescribing a fixed domain set. Domains are user/agent-defined; herrscher
  seeds none by default (an agent may only ever need `dev`).

## Design

### 1. `KindDomain` node (contracts)

Add to `contracts/memory.go`:

```go
// KindDomain is a transverse area-of-concern root (dev, research, …) that groups
// projects and facts topically, above the ownership spine. A project links to
// its domain with "in-domain"; a fact carries Meta["domain"] for filtering.
KindDomain NodeKind = "domain"
```

Domain nodes live at key `domaines/<slug>` (french-consistent with the existing
`projets/` fallback in `scaffold.go`). A domain node is an index: its Body is
human prose, its Links point to the projects/facts it contains
(`Rel: "contains"`), and projects point back (`Rel: "in-domain"`).

### 2. Routing convention

- Every `Record`ed fact MAY carry `Meta["domain"] = "<slug>"`. This is the
  routing tag — it is what makes `Search`/recall filterable by domain (A builds
  ranking on top; C only establishes the tag + node).
- A project is attached to a domain by linking `projet → domaines/<slug>`
  (`in-domain`) and `domaines/<slug> → projet` (`contains`), mirroring how
  `Init` already links org↔project.

### 3. Scaffolding (obsidian-memory)

Extend `InitSpec` in `scaffold.go`:

```go
type InitSpec struct {
	Org     string
	Domain  string   // NEW: optional; "" = no domain layer (current behaviour)
	Project string
	Repos   []string
	Servers []string
}
```

When `Domain != ""`, `Init` additionally:
- `ensure`s a `domaines/<slug>/index` node (`KindDomain`) if absent (idempotent,
  same non-overwriting `ensure` as today);
- links the domain index → project (`contains`) and project → domain
  (`in-domain`);
- stamps `Meta["domain"] = <slug>` on the project index node.

`Domain == ""` reproduces today's exact scaffold — this is a pure superset, no
migration of existing vaults required.

### 4. Query by domain (contracts, minimal)

`contracts.Query` already has `Tags`. Rather than a new field, domain filtering
reuses tag matching by treating `Meta["domain"]` as a first-class tag in
`matchesQuery` (obsidian `memory.go`): a node matches `q.Tags` if the tag is in
its `tags` Meta **or** equals its `domain` Meta. This keeps the contract
unchanged and gives A a ready filter.

## Interfaces changed

| Symbol | Repo | Change |
|--------|------|--------|
| `KindDomain` | contracts | new const |
| `InitSpec.Domain` | obsidian-memory | new field |
| `Init` | obsidian-memory | scaffolds domain node + links when set |
| `matchesQuery` | obsidian-memory | domain Meta counts as a tag |

No breaking changes: additive const, additive struct field (zero value =
current behaviour), widened match (strictly more permissive).

## Testing

- `Init` with `Domain` set creates `domaines/<slug>/index` of `KindDomain`, links
  both directions, and stamps the project's `domain` Meta.
- `Init` with `Domain == ""` is byte-identical to today (regression guard).
- `Init` is idempotent across two calls sharing a domain (second project attaches
  to the existing domain node, no overwrite).
- `Search(Query{Tags: ["dev"]})` returns a node whose only `domain` Meta is `dev`.

## Open question (defer, not blocking)

Is domain worth it for a single-purpose agent (Neublox = Roblox dev only)? Ship
the plumbing minimal; if an agent never sets `Domain`, it pays nothing. Revisit
whether to seed default domains once B populates real content.
