# Memory A — Reliable recall (ranking)

**Date:** 2026-07-02
**Status:** Design (approved for spec)
**PR order:** 3 of 4 (C → B → A → D)
**Repos touched:** `herrscher-obsidian-memory`, `herrscher-contracts` (minor)

## Context

Recall is the weakest link. Two paths exist today:

- `Recall(key, depth)` (`memory.go:136`) — a BFS graph walk from a **known** key.
  Fine, but requires already knowing the key; it dumps the whole reachable
  subgraph with no notion of relevance.
- `Search(Query)` (`memory.go:208`) — walks the **entire** vault
  (`fs.WalkDir`), unmarshals every `.md`, and matches with a naive
  `strings.Contains(Title+Body, q.Text)` (`matchesQuery`). No ranking, no
  recency, no proximity, no scoring. Results are in filesystem-walk order,
  truncated by `q.Limit`.
- `RecallScoped` (`memory_scope.go`) merges the shared + private subgraphs at a
  depth — again, everything reachable, unranked.

Result: once the vault fills (chantier B), the agent gets either an unranked
substring dump or an unranked whole-subgraph dump. "Recall pas fiable."

## Goal

Return the **most relevant** nodes for a turn, ranked — so the context primed
into a prompt is small, ordered, and on-topic.

## Non-goals

- **No embeddings / vector store** in this PR. Pure-Go lexical ranking, zero new
  dependencies. A semantic layer is a deliberate later option, not now (YAGNI).
- No change to the co-edited markdown format (humans still own the files).

## Design

### 1. Ranked `Search`

Replace the boolean `matchesQuery` gate with a **score**; keep the same walk but
sort by score and cut to `Limit`.

`score(node, q) = w_tf·tf + w_title·titleHit + w_recency·recency + w_kind·kindBoost`

- **tf**: term-frequency of `q.Text` tokens in Title+Body (tokenized,
  lowercased). Replaces the single substring check — multi-term, so "nats
  transport" ranks a node mentioning both above one mentioning either.
- **titleHit**: strong boost when a query term is in the Title (titles are the
  stable handle).
- **recency**: from `Meta["capturedAt"]`/date Meta (B stamps `capturedAt`),
  decayed — recent memory outranks stale. Missing date → neutral, never
  penalized to zero.
- **kindBoost**: small per-kind weight (e.g. `decision`/`user` > `session`), so
  durable facts beat transient session logs.

Weights are constants first, tunable via config later. A node with score 0
(no term match and no forced kind/tag) is excluded — preserving today's "must
match" semantics while ordering the matches.

### 2. Targeted `RecallScoped`

Add a ranked variant so priming a turn returns top-K, not the whole subgraph:

```go
// RecallRelevant returns the top-K nodes from the scoped subgraph ranked by
// relevance to `text`, instead of the full merged subgraph. K bounds prompt size.
func RecallRelevant(ctx, m Memory, s MemoryScope, text string, k int) ([]Node, error)
```

Implementation: `RecallScoped` to get the candidate subgraph (shared+private),
then rank its nodes by the same `score` against `text`, return top-K. Graph
**proximity** to a root contributes to the score (nodes closer to the Project/
Agent root rank higher), reusing the BFS depth already computed.

### 3. Contract additions (minimal)

`contracts.Query` gains optional ranking controls (all zero-value = current
behaviour):

```go
type Query struct {
	Text  string
	Kinds []NodeKind
	Tags  []string
	Limit int
	// NEW:
	Ranked bool // when true, results are score-sorted (default false = walk order)
}
```

`Ranked` defaults false so existing `Search` callers are unaffected;
`RecallRelevant` sets it internally. Keeping it a flag (not a new method) avoids
widening the `Memory` port.

### 4. Domain filter (from chantier C)

If C landed, `q.Tags` already matches `Meta["domain"]`, so
`Search(Query{Text, Tags:["dev"], Ranked:true})` gives ranked, domain-scoped
recall for free.

## Interfaces changed

| Symbol | Repo | Change |
|--------|------|--------|
| `Query.Ranked` | contracts | new optional field |
| `RecallRelevant` | contracts (memory_scope.go) | new helper |
| `Search` | obsidian-memory | score + sort when `Ranked` |
| scoring | obsidian-memory | new internal `score()` + tokenizer |

No breaking change: additive field defaulting to old behaviour; new helper
alongside `RecallScoped`.

## Performance note

`Search` still walks the whole vault per call (O(files)). Acceptable at expected
vault sizes (hundreds–low-thousands of small `.md`). If it becomes hot, a later
PR adds an in-memory index built on `New`/invalidated on `Record` — explicitly
out of scope here to keep the change small and reviewable. **Do not silently cap
results**: if `Limit` truncates ranked results, that's fine (top-K by design),
but log nothing-matched vs truncated distinctly.

## Testing

- Two-term query ranks a node containing both terms above one with a single term.
- Title match outranks body-only match of equal frequency.
- More recent node (`capturedAt`) outranks an older node of equal textual score.
- `Ranked:false` reproduces today's walk-order + `Limit` behaviour exactly.
- `RecallRelevant(text, k)` returns ≤ k nodes, highest score first, drawn from
  both shared and private scope; nodes nearer a root rank higher on ties.
- Zero-score (no match) nodes never appear.
