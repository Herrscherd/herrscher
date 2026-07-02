# Memory B — Auto-capture (curation loop)

**Date:** 2026-07-02 (rewritten after reconciling with shipped code)
**Status:** Design (approved for spec)
**PR order:** 2 of 4 (C → B → A → D)
**New repo:** `herrscher-llm-extractor`
**Repos touched:** `herrscher-llm-extractor` (new), `herrscher` (blank import + config)

## Context — what already exists (course-correction)

The first draft of this spec assumed `Consolidate` was an empty seam and proposed
adding a "curation backend" to the orchestrator factory. **That was wrong.** The
curation loop is already built in `herrscher-orchestrator` (v0.1.4):

- `Learner` (`learner.go`) is the richer Orchestrator: same per-turn
  `Context`/`Observe` as the default `Curator`, plus a real `Consolidate`.
- `Consolidate(ctx)` reads the call **journal** file + the session **transcript**
  (`mem.Recall(session, 0).Root.Body`), calls an `Extractor`, and persists each
  returned `Candidate` via the P1 scope helpers `RecordShared`/`RecordPrivate`.
  It is **idempotent** within a session via a `seen` key set.
- `Extractor` is the seam: `Extract(ctx, journal, transcript) ([]Candidate, error)`.
  It is documented as the **closed part of the moat**; the orchestrator package
  ships none.
- `Candidate{Node, Private}` — `Private` routes a node under the Agent (a learned
  skill) vs the Project (a shared fact).
- `RegisterExtractor(name, e)` / `lookupExtractor(name)` (`extractor_registry.go`)
  — the same blank-import plugin pattern as the host registry.
- `register.go` wires it: when `memory.extractor` names a registered extractor,
  the factory builds a `Learner` with `memory.journal` + `memory.consolidate-every`
  (run `Consolidate` every N observed turns; 0 = manual). Otherwise it falls back
  to the plain scoped `Curator`.

**The real gap:** no `Extractor` implementation is registered anywhere, so
`Consolidate` is a no-op and the vault never self-populates. That is exactly
"presque rien dedans."

## Goal

Ship a concrete, **generic LLM-driven** `Extractor` in a new repo
`herrscher-llm-extractor` that turns journal + transcript into memory candidates,
so herrscher autonomously records durable facts (shared) and skills (private).
No Roblox-specific heuristics — that stays the closed Neublox extractor; this is
the open, reusable reference implementation.

## Decisions (confirmed)

- **Where:** a new module `github.com/Herrscherd/herrscher-llm-extractor`,
  requiring `herrscher-orchestrator` (for the `Extractor`/`Candidate` seam +
  `RegisterExtractor`) and `herrscher-contracts` (for `Node`, `Backend`,
  the registry). Open + generic; not in Neublox, not in the orchestrator.
- **Trigger:** keep the existing every-N-turns mechanism in `Learner`. **Zero**
  change to `herrscher-orchestrator` or `herrscher` core wiring beyond a blank
  import and three config keys. No session-close hook.
- **Extraction:** LLM-driven via the existing `contracts.Backend` port.
- **Write autonomy:** auto-write above a confidence threshold (filtered inside
  the extractor, since `Learner` persists whatever it returns). Stable keys make
  writes idempotent across sessions; provenance Meta makes them auditable.

## Design

### 1. Module layout

```
herrscher-llm-extractor/
  go.mod                 // module …/herrscher-llm-extractor; requires orchestrator + contracts
  extractor.go           // LLMExtractor: implements orchestrator.Extractor
  backend.go             // lazy backend acquisition from the contracts registry
  prompt.go              // extraction prompt + JSON schema instructions
  parse.go               // tolerant JSON → []Candidate, stable key + provenance
  register.go            // init(): RegisterExtractor("llm", …)
  *_test.go
```

### 2. `LLMExtractor` (extractor.go)

```go
// LLMExtractor is the open, reference Extractor: it asks a contracts.Backend to
// distill a stretch of work (journal + transcript) into memory candidates.
type LLMExtractor struct {
	backend   contracts.Backend // the curation model edge
	threshold float64           // drop candidates below this confidence
	max       int               // cap candidates per Consolidate (0 = no cap)
}

// New builds an extractor over an explicit backend (used in tests and by callers
// that already hold a model edge).
func New(b contracts.Backend, opts ...Option) *LLMExtractor { … }

func (e *LLMExtractor) Extract(ctx context.Context, journal, transcript string) ([]orchestrator.Candidate, error) {
	if e.backend == nil || (strings.TrimSpace(journal) == "" && strings.TrimSpace(transcript) == "") {
		return nil, nil // nothing to curate → clean no-op (Consolidate stays best-effort)
	}
	raw, err := e.backend.Respond(ctx, extractionPrompt(journal, transcript), nil)
	if err != nil {
		return nil, err
	}
	return parseCandidates(raw, e.threshold, e.max), nil // tolerant; never fatal on bad JSON
}
```

`var _ orchestrator.Extractor = (*LLMExtractor)(nil)`.

### 3. Backend acquisition (backend.go) — the one real design choice

The `Extractor` seam gets **no** backend or config injected (registration is a
blank-import `init()`; `register.go` looks up a bare `Extractor`). So the default
registered extractor builds its **own** backend, lazily, mirroring the host's
`firstBackend` exactly:

```go
// lazyBackend builds a curation backend on first use from the registered backend
// plugin, exactly as the host does (Resolve(manifest.Config, os.Getenv) →
// factory), then caches it. A curation-specific model override lets curation run
// on a cheaper/faster model than the conversation backend.
func lazyBackend() (contracts.Backend, error) {
	for _, p := range contracts.Default.Backends() {
		if p.Backend == nil { continue }
		cfg, err := contracts.Resolve(p.Manifest.Config, curationEnv) // env with model override
		if err != nil { return nil, err }
		return p.Backend(context.Background(), cfg)
	}
	return nil, nil // no backend registered → Extract no-ops, memory still recalls
}
```

- `curationEnv` reads normal env but overrides the model key with
  `HERRSCHER_CURATION_MODEL` when set, so curation ≠ conversation model.
- Built once (guarded), cached on the registered extractor.
- No backend registered → `lazyBackend` returns `(nil, nil)` → `Extract` no-ops.
  Recall still works; only self-population is skipped. Same graceful degrade as
  today.

This keeps the extractor a pure blank-import plugin: **no orchestrator, contracts,
or core code changes** — only the host's plugin list and config.

### 4. Extraction contract (prompt.go → parse.go)

The prompt instructs the model to return a JSON array of candidates, mirroring
the memory-writing guidance herrscher already follows (one fact per node,
`**Why:** / **How to apply:**` for decision/feedback kinds, link liberally):

```json
[{
  "kind": "decision|architecture|user|production|…",
  "title": "short stable title",
  "body": "the fact in markdown; why it matters; how to apply",
  "domain": "dev",                     // optional → Meta["domain"] (feeds chantier C)
  "tags": ["nats","transport"],
  "links": [{"to":"projects/x/index","rel":"applies-to"}],
  "private": false,                    // true → RecordPrivate (agent skill)
  "confidence": 0.0
}]
```

`parseCandidates` is **tolerant**: a malformed element is skipped, never fatal;
below-threshold and over-`max` elements are dropped. For each survivor it builds
an `orchestrator.Candidate`:

- **Stable Key** — deterministic slug from `kind` + `title` (e.g.
  `facts/<kind>/<slug(title)>`), so the same fact re-derived in a later session
  **upserts** by Key (`Record` is upsert-by-Key) instead of duplicating. This is
  what makes auto-capture safe across sessions, complementing the Learner's
  per-session `seen`.
- **Provenance Meta** — `Meta["capturedBy"] = "llm-extractor"` and
  `Meta["domain"]` from the candidate. (`originSessionId` is **deferred**: the
  `Extractor` signature carries no session id and we are not touching the
  orchestrator; if wanted later, the `Learner` can stamp it before persisting.)
- `Node.Kind` maps from the string; an unknown/blank kind falls back to
  `KindSession` (transient) rather than being dropped.

### 5. Wiring in the host (`herrscher`)

- Add a blank import of `herrscher-llm-extractor` to the host's generated
  plugins list (next to the orchestrator import).
- Set config: `memory.extractor = "llm"`, `memory.journal = <worktree>/.neublox/calls.log`,
  `memory.consolidate-every = <N>` (e.g. 10). These already flow through
  `register.go`; no host code beyond config + the import.
- `HERRSCHER_CURATION_MODEL` optionally selects a cheaper curation model.

## Interfaces changed

| Symbol | Repo | Change |
|--------|------|--------|
| `LLMExtractor`, `New`, `Option`s | herrscher-llm-extractor (new) | concrete `orchestrator.Extractor` |
| `init()` → `RegisterExtractor("llm", …)` | herrscher-llm-extractor (new) | registers the default |
| blank import + 3 config keys | herrscher | opt the host into auto-capture |

**No change** to `herrscher-orchestrator`, `herrscher-contracts`, or
`herrscher-obsidian-memory`. The seam, the scope helpers, and the trigger are
already shipped — B only fills the empty extractor slot.

## Testing (in herrscher-llm-extractor)

- Fake `contracts.Backend` returning 2 valid candidates → `Extract` yields 2
  `Candidate`s with stable Keys, `capturedBy` Meta set, `Private` routed per JSON.
- A candidate below `threshold` is dropped; over `max` are cut.
- A malformed JSON element is skipped; valid siblings still returned; no error.
- Backend returns non-JSON garbage → `parseCandidates` returns empty, no panic,
  no error (best-effort).
- `Extract` with empty journal **and** empty transcript → `(nil, nil)` no-op.
- Same title+kind across two `Extract` calls → identical Key (upsert-safe).
- Nil backend → `Extract` returns `(nil, nil)`.
- Integration: a `Learner` (real orchestrator) with the fake-backed extractor,
  after `Observe`×N, has recorded the expected shared/private nodes (exercises the
  real Consolidate + scope helpers end to end).

## Open questions (non-blocking)

- Curation model default: lean on the conversation backend's default model unless
  `HERRSCHER_CURATION_MODEL` overrides. Confirm the env key name against the
  Claude backend's manifest during the plan.
- `max` default (candidates per Consolidate): start at e.g. 8 to bound cost; log
  when truncated rather than silently cap.
