# G7 — Raw-session archival tier (FTS-searchable) — design

**Date:** 2026-07-30
**Status:** Design (approved 2026-07-30)
**Slice:** G7 of the Hermes-parity roadmap
(`docs/superpowers/specs/2026-07-30-memory-learning-hermes-parity-design.md`)
**Repos:** new plugin `herrscher-fts-memory` (see §2 for why not
`herrscher-obsidian-memory`). `herrscher-contracts` gains one small optional
capability port. `herrscher-orchestrator` gains one best-effort write-path call.

## Goal

Give herrscher a raw, full-text-searchable archive of session transcripts —
the Hermes `state.db` (SQLite FTS5) analogue — as a **separate tier** from the
distilled node graph. The node graph (`contracts.Memory` / obsidian vault) is
curated facts and skills; G7 does not touch it. It adds a second, independent
recall surface: "what did we actually say, verbatim, in any past session,"
searchable by keyword even when nothing was ever distilled into a node.

`memory search` must, after this slice, return hits from raw transcripts in
addition to node hits — without conflating the two, and without any node-graph
code needing to know the raw tier exists.

## Baseline (verified in code, 2026-07-30)

- `contracts.Memory` (`memory.go`) is `Recall/Record/Search/Links/Close`,
  storage-neutral, over `Node`/`Query`/`Subgraph`. `Query` has no field for
  "raw" — it selects `Node`s by `Text/Kinds/Tags`. `Search` returns
  `[]contracts.Node`; a raw transcript snippet is not a `Node` (no stable Key,
  no Kind, no Links) and forcing it into one would be a lossy fit.
- Optional capabilities already follow a fixed pattern in `contracts`:
  `Provisioner`, `Locator`, `Deleter` — small interfaces the obsidian plugin
  implements and callers reach via type-assertion, *not* part of the `Memory`
  interface itself. G7 adds one more optional port in this family rather than
  widening `Memory` or `Query`.
- `herrscher-obsidian-memory/memory.go`: `Search` (line 292) does a
  `fs.WalkDir` over the vault, parses each `.md` file (with an mtime/size
  parse cache), and matches in Go (`matchesQuery`); `Ranked` scores via
  `contracts.Score`. There is **no index of any kind** — no SQLite, no bleve,
  no inverted index. `go.mod` for obsidian has exactly one dependency:
  `herrscher-contracts`. This is a deliberately thin, dependency-free plugin;
  it is the wrong place to bolt on a binary search index.
- `herrscher-orchestrator/learner.go` line 116-120: `Consolidate` reads
  `l.readJournalTail()` (best-effort, byte-offset tracked) and the transcript
  via `l.mem.Recall(ctx, l.session, 0).Root.Body`, then feeds both to
  `l.extract.Extract(ctx, journal, transcript)`. This is the exact raw
  material G7 must index — no new plumbing is needed to *obtain* the
  transcript, only to *route* it to a raw-index sink at the end of
  `Consolidate`.
- No `memory search` CLI verb exists yet anywhere in the host tree (grepped
  `plugins/`, `core/host/`, `pluginhost.go`) — it exists nowhere today.
  **Resolved: G7 creates this new host CLI verb** as part of this slice. This
  spec defines the contract the verb consumes; wiring the verb itself is host
  work delivered under this same slice.

## Design

### 1. Storage/index engine — recommend `modernc.org/sqlite`, pure-Go FTS5

Three options were weighed:

- **`mattn/go-sqlite3`** (cgo) — mature FTS5 support, but requires a C
  toolchain at build time. The host CI fetches modules with no auth and must
  stay green on whatever runners it has; cgo turns a `go build` into a
  toolchain dependency and complicates cross-compilation. Rejected.
- **`blevesearch/bleve`** (pure Go) — full-featured but a large dependency
  tree (its own index format, analyzers, query language) that has nothing in
  common with the rest of the stack. Heavier than the problem warrants for
  "index a transcript, search it with keywords."
- **`modernc.org/sqlite`** (pure Go, transpiled from SQLite's C source,
  **includes FTS5**) — Recommended. Zero cgo, a single new dependency, and it
  gives an actual SQL `CREATE VIRTUAL TABLE ... USING fts5(...)` — the closest
  drop-in analogue to Hermes' own `state.db`. Matches the roadmap's explicit
  reference point and keeps the new plugin's dependency footprint to one
  well-maintained module.

`herrscher-fts-memory` therefore depends on `herrscher-contracts` +
`modernc.org/sqlite`. `herrscher-obsidian-memory`'s zero-extra-dep property is
untouched.

### 2. Where the tier lives — new plugin `herrscher-fts-memory`, not obsidian

The roadmap text allows either. Recommend a **new plugin**:

- Obsidian's entire design center is "vault of markdown files, human-openable,
  zero binary state" (`Locator` even hands back `obsidian://` URIs). An FTS5
  index is a binary SQLite file with no markdown representation — bolting it
  onto obsidian would mean the "obsidian" plugin ships a dependency and a data
  file that has nothing to do with Obsidian-the-app.
- The two tiers have different write cadences (obsidian: per-Record, small;
  FTS: per-session, transcript-sized), different lifecycles (node graph is
  curated/mutable; raw archive is append/replace-by-session, never merged),
  and different optionality (a deployment can run without the raw tier
  entirely — the roadmap already calls G7 "independent, least urgent").
- A separate plugin means the host can omit it from a build with zero blast
  radius (matches invariant 1: storage stays in a plugin, and *which* plugins
  are loaded stays a host/deployment choice).

### 3. The port shape — new optional capability, not a `Query` flag

A `Query.IncludeRaw bool` flag was considered and rejected: `Memory.Search`
returns `[]contracts.Node`, and a raw transcript hit is not a `Node` — it has
no stable `Key` identity, no `Kind`, no `Links`, and its natural result shape
is "session id + matched snippet(s) + rank," not a graph node. Forcing it
through `Node` would mean inventing a fake `Kind` (e.g. `"raw"`) and stuffing
the snippet into `Body`, which then pollutes every other `Search` caller that
assumes `Kinds` filters are exhaustive over the graph.

Instead, G7 adds one new optional capability port in `herrscher-contracts`,
in the same family as `Provisioner`/`Locator`/`Deleter` — implemented by
`herrscher-fts-memory`, reached via type-assertion, absent everywhere else:

```go
// RawHit is one full-text match against an archived raw session transcript.
// It is deliberately not a Node: it has no stable Key, no Kind, no Links —
// only enough to let a human or agent jump to the source session.
type RawHit struct {
    SessionKey string  // the session this hit came from (matches Learner's l.session)
    Snippet    string  // a short excerpt around the match, for display
    Score      float64 // FTS5 bm25 rank (lower is better, per SQLite convention) or 0 if unranked
}

// RawQuery selects raw-session hits by keyword. Mirrors Query's shape where it
// makes sense (Text, Limit) but carries no Kinds/Tags — raw sessions are
// untyped by design.
type RawQuery struct {
    Text  string
    Limit int // 0 = no limit
}

// RawIndexer is an OPTIONAL Memory capability (same pattern as Provisioner /
// Locator / Deleter): a raw full-text archive of session transcripts,
// separate from the curated node graph. A Memory implementation that has no
// raw tier simply does not implement it; callers type-assert and degrade to
// node-only search.
type RawIndexer interface {
    // IndexSession (re-)indexes one session's transcript in full. Re-indexing
    // an already-archived session REPLACES its prior content rather than
    // appending — see Idempotence.
    IndexSession(ctx context.Context, sessionKey, transcript string) error
    // SearchRaw finds archived transcript snippets matching q, most relevant
    // first when the engine can rank (FTS5 bm25).
    SearchRaw(ctx context.Context, q RawQuery) ([]RawHit, error)
}
```

This keeps `Memory`, `Query`, and `Node` completely unchanged — no
`herrscher-obsidian-memory` code is touched, and every existing `Search`
caller is unaffected (invariant 1: ports only, policy not engine).

### 4. The index-write path — best-effort, end of `Consolidate`, keyed by session

`herrscher-fts-memory` is wired into the orchestrator the same way a
`Merger`/`Consolidator` is discovered today (G1/G2 pattern): the `Learner`
holds an optional raw-index sink and type-asserts for it, so a build without
the plugin loaded is a clean no-op.

```go
// rawIndexer returns the Learner's raw-archive sink if one is wired, mirroring
// the merger()/consolidator() discovery pattern.
func (l *Learner) rawIndexer() (contracts.RawIndexer, bool) {
    ri, ok := l.raw.(contracts.RawIndexer) // l.raw set by the host factory when the FTS plugin is loaded
    return ri, ok
}
```

Trigger: **on each `Consolidate`**, not on session end — `Consolidate` already
runs out-of-band on a cadence (`memory.consolidate-every`) and already has the
transcript in hand (`l.mem.Recall(ctx, l.session, 0).Root.Body`, learner.go
line 118-119). Piggybacking here means no new trigger, no new lifecycle event,
and the raw archive is naturally kept fresh at the same cadence as everything
else the Learner does — “on session end” would require a new hook the host
does not currently fire, and “per-Record” would run once per fact instead of
once per transcript, both unnecessary complexity for a least-urgent slice.

```go
// end of Consolidate, after Sweep/Merge (learner.go), best-effort:
if ri, ok := l.rawIndexer(); ok {
    if err := ri.IndexSession(ctx, l.session, transcript); err != nil {
        // best-effort: log-and-continue only, never returned as firstErr
        l.logf("raw-index: %v", err)
    }
}
```

`transcript` is the exact value already computed at line 118-119 — no new
Recall, no new read of the journal. A missing/empty transcript is simply
indexed as empty content (SQLite FTS5 handles empty documents fine) or
skipped entirely if `transcript == ""` (cheap short-circuit, avoids a useless
write on a session with no recalled body yet).

### 5. `herrscher-fts-memory` internals (new plugin, sketch)

```go
package ftsmemory

type FTSMemory struct {
    db *sql.DB // modernc.org/sqlite, one file e.g. <vault-root>/.fts/sessions.db
}

func Open(path string) (*FTSMemory, error) // runs the schema migration below

// schema (created once, IF NOT EXISTS):
//   CREATE VIRTUAL TABLE sessions USING fts5(session_key UNINDEXED, body);
```

`IndexSession` runs `DELETE FROM sessions WHERE session_key = ?` then
`INSERT INTO sessions(session_key, body) VALUES (?, ?)` inside one
transaction — delete-then-insert, not append, so re-indexing is idempotent
(§ Idempotence). `SearchRaw` runs
`SELECT session_key, snippet(sessions, 1, '', '', '...', 8), bm25(sessions) FROM sessions WHERE sessions MATCH ? ORDER BY bm25(sessions) LIMIT ?`
and maps rows to `[]RawHit`.

### 6. Surfacing via `memory search`

G7 creates the new host CLI verb `memory search` (it exists nowhere today).
The verb calls `Memory.Search` for node hits as today,
then separately type-asserts the loaded memory/raw service for
`contracts.RawIndexer` and calls `SearchRaw`. Output renders the two as
visually distinct sections so a caller never mistakes a raw snippet for a
curated fact:

```
$ memory search "flaky retry"

Nodes:
  [decision] retry-backoff-policy — ...

Raw sessions:
  [session:2026-07-14T09-called-agent] "...added a flaky retry around the..."
  [session:2026-06-30T22-called-agent] "...the retry logic keeps flaking on..."
```

Node hits keep their existing `contracts.Node` shape; raw hits are rendered
from `RawHit{SessionKey, Snippet}` with a `[session:...]` prefix instead of a
`[kind]` prefix — the two are never merged into one ranked list, satisfying
the roadmap's framing that this is a *separate* tier, not an extension of the
node graph's relevance ranking.

## Config surface

| Setting key | Env | Default | Meaning |
|---|---|---|---|
| `fts.enabled` | `MEMORY_FTS_ENABLED` | `false` | gate on the whole raw tier; `false` → plugin not loaded / `rawIndexer()` absent → clean no-op everywhere. |
| `fts.db-path` | `MEMORY_FTS_DB_PATH` | `<vault-root>/.fts/sessions.db` | where the SQLite FTS5 file lives; relative paths resolve under the vault root. |
| `fts.snippet-chars` | `MEMORY_FTS_SNIPPET_CHARS` | `160` | approx. snippet length returned per `RawHit` (maps to the FTS5 `snippet()` token-count argument, converted). |

No new `herrscher-orchestrator` setting is strictly required beyond wiring
`l.raw` in the host factory when `fts.enabled` is true — the existing
`memory.consolidate-every` cadence already governs when `IndexSession` runs.

## Idempotence

Re-indexing the same session must not duplicate rows or search hits:

- `IndexSession` is **delete-then-insert per `session_key`**, inside one
  transaction — never an `INSERT`-only append. A session consolidated 10
  times over its lifetime always has exactly one row (one FTS5 document) for
  its `session_key`.
- The transcript body itself is authoritative each time (whatever
  `l.mem.Recall(...).Root.Body` currently holds), so a growing transcript
  across consolidations is captured correctly — the archive always reflects
  the latest full transcript, not a stale first snapshot and not duplicated
  fragments.
- No separate `seen`/offset tracking is needed for the raw tier (unlike the
  journal-tail byte offset the Learner already tracks for extraction) because
  the write is a full replace, not an append — a second identical
  `IndexSession` call for an unchanged transcript is a no-op in effect (same
  content rewritten), which is safe and cheap for the volumes involved
  (one transcript per session, not per turn).

## Testing

- **`herrscher-contracts`:** compile-only check that `RawIndexer` is not part
  of `Memory` (a type asserting `Memory` alone must not require it); `RawHit`/
  `RawQuery` are plain structs, no behavior to unit test there.
- **`herrscher-fts-memory` (new repo):**
  - `IndexSession` then `SearchRaw` finds the indexed text by keyword.
  - Re-`IndexSession`-ing the same `sessionKey` with different content leaves
    exactly one row for that key and `SearchRaw` reflects only the latest
    content (old content no longer matches).
  - `SearchRaw` on an empty/fresh db returns `(nil, nil)`, not an error.
  - `RawQuery.Limit` caps result count; `Limit == 0` returns all matches.
  - Concurrent `IndexSession` calls for different `sessionKey`s do not corrupt
    each other's rows (basic concurrency smoke test given `database/sql`'s
    connection pooling).
  - Schema migration (`Open`) is idempotent — opening an existing db file
    twice does not error or reset data.
- **`herrscher-orchestrator`:**
  - No `RawIndexer` wired → `Consolidate` behaves exactly as before G7 (no
    call attempted, no error path exercised) — pure no-op, matches the G1/G2
    discovery-pattern tests.
  - `RawIndexer` wired + fake returning an error → `Consolidate`'s overall
    result is unaffected (best-effort; error is logged, not propagated).
  - `RawIndexer` wired, happy path → spy records exactly one `IndexSession`
    call per `Consolidate` invocation, with `sessionKey == l.session` and
    `transcript` equal to the value from `Recall(...).Root.Body`.
  - Empty transcript (`""`) → `IndexSession` is skipped (spy asserts zero
    calls), avoiding a useless write.
- **Host (`memory search` verb):** with both a node hit and a raw hit
  present, output renders both sections; with the FTS tier disabled, output
  renders only the node section (no error, no empty "Raw sessions:" noise).

## Release footprint

- **`herrscher-contracts`** → next minor (v0.2.10): adds `RawHit`, `RawQuery`,
  `RawIndexer` only — pure addition, no existing type changes. No breaking
  change for `herrscher-obsidian-memory` or `herrscher-orchestrator`.
- **`herrscher-fts-memory`** → **new module**,
  `github.com/Herrscherd/herrscher-fts-memory`, v0.1.0. Depends on
  `herrscher-contracts` v0.2.10 + `modernc.org/sqlite`.
- **`herrscher-orchestrator`** → next minor (v0.1.13): `rawIndexer()`
  discovery + the best-effort `Consolidate` call + `l.raw` field. Depends
  only on the already-released contracts v0.2.10.
- **Host** (`plugins.go` / `pluginhost.go`): register `herrscher-fts-memory`
  as an optional plugin module (loaded only when `fts.enabled`); add the
  new `memory search` CLI verb (created by this slice — it exists nowhere
  today) to merge node + raw results per §6. Bump orchestrator + contracts pins.
  `herrscher-fts-memory` must be a public repo per the existing host-deps
  rule (host CI fetches with no auth) — same constraint every other plugin
  module already satisfies.
- README "Learning" section gains a **Raw-session archive (FTS)** paragraph;
  the memory-vs-hermes gap note marks G7 shipped.

## Out of scope (YAGNI)

- **Cross-session semantic/embedding search over raw transcripts** — G7 is
  keyword FTS5 only, matching Hermes' own `state.db`. Embedding-based recall
  over raw text is a possible future slice, not this one.
- **Automatic pruning/retention of old raw sessions** — the archive grows
  unbounded for now (SQLite handles this volume fine); a retention policy
  (e.g. archive-after N days mirroring G3/G4's node lifecycle) is a later
  refinement if disk becomes a real concern.
- **Merging raw hits into the same ranked list as node hits** — deliberately
  kept as two labeled sections (§6); unifying ranking across two structurally
  different result types is unnecessary complexity for a least-urgent slice.
- **A restore/delete verb for raw sessions** — `RawIndexer` has no `Delete`
  counterpart yet; add one only if a concrete need for forgetting a raw
  session (privacy, size) arises. Until then the archive is append-only in
  spirit (replace-on-reindex, never pruned).
- **Wiring the raw tier into `Learner.Extract`/`Merge`** — G7 is a pure
  archival/search sink, not an input to distillation; the node graph's
  extraction path is untouched.

## Invariants (from the umbrella roadmap)

1. **Ports only, policy not engine** — G7 adds one optional capability port
   (`RawIndexer`) in the same family as `Provisioner`/`Locator`/`Deleter`;
   `Memory`, `Query`, and `Node` are unchanged. Storage (the SQLite FTS5 file)
   stays entirely inside the new `herrscher-fts-memory` plugin; the
   orchestrator only discovers and calls the port.
2. **Learning never breaks a turn** — `IndexSession` is called at the very
   end of the already-out-of-band `Consolidate`, and its error is logged, not
   returned as `firstErr`; a raw-index failure can never fail or block a user
   turn, and never affects the Learner's existing extract/merge/sweep result.
3. **Reversible over destructive** — indexing is delete-then-insert per
   session (never a destructive edit of the node graph or of other sessions'
   rows), and the raw tier is strictly additive/replaceable: no slice of the
   existing curated memory is deleted, altered, or reinterpreted by G7.
