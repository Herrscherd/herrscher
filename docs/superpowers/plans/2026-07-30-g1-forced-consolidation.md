# G1 Forced Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the `Learner` respond to a per-node budget refusal by consolidating the candidate to fit and retrying — instead of silently dropping it and aborting the whole pass.

**Architecture:** All changes live in `herrscher-orchestrator/learner.go` (+ a new test file). A new `Consolidator` seam is discovered by type-asserting the existing `extract Extractor`. A budget-aware `persist` helper catches `*contracts.BudgetError`, asks the consolidator to shrink the node to the error's `Limit`, and retries once; unresolvable candidates go to an in-memory `pending` queue drained at the top of each pass. The persist loop no longer aborts on the first error.

**Tech Stack:** Go 1.25, `github.com/Herrscherd/herrscher-contracts` (v0.2.9, already released — provides `BudgetError`, `RecordShared`/`RecordPrivate`), stdlib `log/slog` for the WARN, `unicode/utf8` for the rune re-check.

## Global Constraints

- **Repo scope:** `herrscher-orchestrator` only. No `herrscher-contracts` and no `herrscher-obsidian-memory` change. Release orchestrator → **v0.1.10**; host `go.mod` bumps orchestrator only (v0.1.9 → v0.1.10).
- **Invariant — ports only:** no new `contracts.Memory` method, no new storage engine. The consolidator is a seam plus plumbing over the existing `Memory` port.
- **Invariant — learning never breaks a turn:** every new path stays inside the best-effort `Consolidate`, whose result is swallowed by `Observe` (`_ = l.Consolidate(ctx)`). The final `_ = l.Sweep(ctx)` must always run.
- **Invariant — reversible over destructive:** a refused candidate is merged-to-fit or held for retry; never truncated in place, never hard-deleted. The raw journal on disk stays the source of record.
- **No new constructor param and no new env flag:** `NewLearner`'s signature is unchanged; the presence of a `Consolidator` is the switch.
- **Dedup the queue by `Node.Key`** so a chronically-unmergeable fact cannot grow `pending` unbounded.
- **WARN, don't drop silently:** every enqueue emits `slog.Warn` carrying key/runes/limit.
- Match the file's existing style: package `orchestrator`, tab indent, comments explaining *why* (not what), best-effort error prose consistent with the existing `Consolidate`/`Sweep` comments.

---

### Task 1: No-abort persist loop (robustness fix)

Mirror the sweep fix (`sweep.go`, commit 9198ca6) in `Consolidate`: a single candidate's `Record` failure must not drop its siblings or skip the sweep. This task changes only the loop's error handling — no budget logic yet.

**Files:**
- Modify: `learner.go` (the `Consolidate` loop, `learner.go:85-104`)
- Test: `learner_test.go` (add one test; reuses the existing `recMem`/`fakeExtractor`)

**Interfaces:**
- Consumes: `contracts.RecordPrivate`/`RecordShared` (unchanged), existing `recMem`, `fakeExtractor`.
- Produces: `Consolidate` now returns the **first** persist error (not necessarily the last-encountered), and always reaches `l.Sweep`. Task 2 relies on this loop shape.

- [ ] **Step 1: Write the failing test**

Add to `learner_test.go`. It needs a memory whose `Record` fails for one specific key. The existing `recMem.Record` never fails, so add an optional `failOn` field to `recMem` (one-line change to the struct + its `Record`):

In `recMem` struct add field:
```go
	failOn string // key whose Record returns an error (node not stored)
```
In `recMem.Record`, before storing:
```go
func (m *recMem) Record(_ context.Context, n contracts.Node) error {
	if n.Key == m.failOn {
		return errors.New("record failed")
	}
	m.nodes[n.Key] = n
	return nil
}
```
Add `"errors"` to `learner_test.go`'s imports.

Then the test:
```go
func TestConsolidateContinuesPastRecordError(t *testing.T) {
	mem := newRec()
	mem.failOn = "facts/eco" // the shared fact fails; the private skill must still persist
	ex := &fakeExtractor{}
	l := NewLearner(mem, "alpha", contracts.MemoryScope{Project: "projects/g", Agent: "agents/s"}, ex, "", 0)

	err := l.Consolidate(context.Background())
	if err == nil {
		t.Fatal("expected the failing candidate's error to be returned")
	}
	// A per-candidate failure must not abort the batch: the sibling still persists.
	if _, ok := mem.nodes["skills/ds"]; !ok {
		t.Fatal("candidate after the failing one was skipped")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test -run TestConsolidateContinuesPastRecordError ./...`
Expected: FAIL — the current loop `return err`s on the first failure, so `skills/ds` is never recorded (the assertion `candidate after the failing one was skipped` fires). (If `fakeExtractor` returns the private skill *before* the shared fact, this still fails because the first branch to error aborts; order in `fakeExtractor` is fact-then-skill, so the fact fails first.)

- [ ] **Step 3: Rewrite the Consolidate loop to accumulate the first error**

Replace the loop body + return in `Consolidate` (`learner.go:85-104`) with:
```go
	var firstErr error
	for _, c := range cands {
		if l.seen[c.Node.Key] {
			continue // already persisted this session — keep Consolidate idempotent
		}
		var err error
		if c.Private {
			err = contracts.RecordPrivate(ctx, l.mem, l.scope, c.Node)
		} else {
			err = contracts.RecordShared(ctx, l.mem, l.scope, c.Node)
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err // record the first failure and keep going: one bad
			} // candidate must not drop its siblings or skip the sweep below
			continue
		}
		l.seen[c.Node.Key] = true
	}
	// Best-effort staleness sweep at the end of a consolidation pass. A sweep
	// error must never propagate out of Consolidate (invariant: learning never
	// breaks a turn).
	_ = l.Sweep(ctx)
	return firstErr
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./...`
Expected: PASS (new test + all existing learner/sweep tests). Then `gofmt -l learner.go learner_test.go` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
git add learner.go learner_test.go
git commit -m "fix(learner): don't abort Consolidate batch on first Record error"
```

---

### Task 2: Consolidator seam + budget catch + retry queue

Add the `Consolidator` seam, the budget-aware `persist`/`record` helpers, the `pending` queue with dedup, and the drain at the top of `Consolidate`. This refactors Task 1's inline loop into the `persist` helper.

**Files:**
- Modify: `learner.go` (imports; new `Consolidator` interface; `errEnqueue` sentinel; `pending` field on `Learner`; new `consolidator`/`record`/`persist`/`enqueue`/`drain` methods; rewire `Consolidate`)
- Test: `learner_budget_test.go` (Create — new fakes + budget tests)

**Interfaces:**
- Consumes: `contracts.BudgetError{Key, Runes, Limit}` (has `Error()`, works with `errors.As`); `contracts.RecordPrivate`/`RecordShared`; the Task 1 loop shape (first-error accumulation, sweep always runs).
- Produces:
  - `type Consolidator interface { Consolidate(ctx context.Context, over contracts.Node, limit int) (contracts.Node, error) }`
  - `func (l *Learner) persist(ctx context.Context, c Candidate) error` — nil on success, `errEnqueue` when a budget refusal is unresolved now, else the underlying non-budget error.
  - `Learner.pending []Candidate` (in-memory, per-session).

- [ ] **Step 1: Write the failing tests**

Create `learner_budget_test.go`. It defines a budget-enforcing memory and a fake consolidator, then covers the five behaviours from the spec.

```go
package orchestrator

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

// budgetMem records nodes but refuses any body longer than `limit` runes with a
// *contracts.BudgetError — the same contract the obsidian per-node budget uses.
type budgetMem struct {
	nodes map[string]contracts.Node
	limit int
}

func newBudgetMem(limit int) *budgetMem {
	return &budgetMem{nodes: map[string]contracts.Node{}, limit: limit}
}

func (m *budgetMem) Record(_ context.Context, n contracts.Node) error {
	if r := len([]rune(n.Body)); m.limit > 0 && r > m.limit {
		return &contracts.BudgetError{Key: n.Key, Runes: r, Limit: m.limit}
	}
	m.nodes[n.Key] = n
	return nil
}
func (m *budgetMem) Recall(_ context.Context, key string, _ int) (contracts.Subgraph, error) {
	return contracts.Subgraph{Root: contracts.Node{Key: key}}, nil
}
func (m *budgetMem) Search(context.Context, contracts.Query) ([]contracts.Node, error) {
	return nil, nil
}
func (m *budgetMem) Links(context.Context, string, string, string) error { return nil }
func (m *budgetMem) Close() error                                        { return nil }

// oneBig returns a single over-budget shared fact from the journal/transcript.
type oneBig struct{ body string }

func (e *oneBig) Extract(context.Context, string, string) ([]Candidate, error) {
	return []Candidate{{Node: contracts.Node{Key: "facts/big", Body: e.body}}}, nil
}

// bigThenSmall returns an over-budget fact followed by an in-budget one, to prove
// a refusal does not drop the sibling.
type bigThenSmall struct{ big, small string }

func (e *bigThenSmall) Extract(context.Context, string, string) ([]Candidate, error) {
	return []Candidate{
		{Node: contracts.Node{Key: "facts/big", Body: e.big}},
		{Node: contracts.Node{Key: "facts/small", Body: e.small}},
	}, nil
}

// shrinkingExtractor is an Extractor that ALSO implements Consolidator: it
// shrinks the over-budget node's body to `to`. If to == "" it returns the node
// unchanged (simulating a consolidator that cannot shrink far enough).
type shrinkingExtractor struct {
	oneBig
	to    string
	calls int
}

func (e *shrinkingExtractor) Consolidate(_ context.Context, over contracts.Node, _ int) (contracts.Node, error) {
	e.calls++
	if e.to == "" {
		return over, nil // still too large
	}
	over.Body = e.to
	return over, nil
}

func mustScope() contracts.MemoryScope {
	return contracts.MemoryScope{Project: "projects/g", Agent: "agents/s"}
}

func TestPersistConsolidatesOverBudgetCandidate(t *testing.T) {
	mem := newBudgetMem(5)                                  // 5-rune limit
	ex := &shrinkingExtractor{oneBig: oneBig{body: "0123456789"}, to: "ok"} // 10 → "ok"
	l := NewLearner(mem, "s", mustScope(), ex, "", 0)

	if err := l.Consolidate(context.Background()); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}
	if ex.calls != 1 {
		t.Fatalf("consolidator not invoked exactly once: calls=%d", ex.calls)
	}
	n, ok := mem.nodes["facts/big"]
	if !ok {
		t.Fatal("consolidated candidate was not persisted")
	}
	if n.Body != "ok" {
		t.Fatalf("persisted body = %q, want the shrunk %q", n.Body, "ok")
	}
	if len(l.pending) != 0 {
		t.Fatalf("queue should be empty after a resolved refusal: %d", len(l.pending))
	}
}

func TestPersistEnqueuesWhenNoConsolidator(t *testing.T) {
	mem := newBudgetMem(5)
	ex := &bigThenSmall{big: "0123456789", small: "hi"} // plain Extractor, no Consolidate
	l := NewLearner(mem, "s", mustScope(), ex, "", 0)

	if err := l.Consolidate(context.Background()); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}
	// Sibling under budget still persists...
	if _, ok := mem.nodes["facts/small"]; !ok {
		t.Fatal("in-budget sibling was dropped")
	}
	// ...the over-budget one is queued, not persisted, not seen.
	if _, ok := mem.nodes["facts/big"]; ok {
		t.Fatal("over-budget candidate was persisted despite no consolidator")
	}
	if len(l.pending) != 1 || l.pending[0].Node.Key != "facts/big" {
		t.Fatalf("over-budget candidate not queued: %+v", l.pending)
	}
	if l.seen["facts/big"] {
		t.Fatal("queued candidate must not be marked seen")
	}
}

func TestPersistEnqueuesWhenConsolidatorStillTooLarge(t *testing.T) {
	mem := newBudgetMem(5)
	ex := &shrinkingExtractor{oneBig: oneBig{body: "0123456789"}, to: ""} // returns node unchanged
	l := NewLearner(mem, "s", mustScope(), ex, "", 0)

	if err := l.Consolidate(context.Background()); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}
	if ex.calls != 1 {
		t.Fatalf("consolidator should be tried once: calls=%d", ex.calls)
	}
	if _, ok := mem.nodes["facts/big"]; ok {
		t.Fatal("still-too-large node must not be persisted")
	}
	if len(l.pending) != 1 {
		t.Fatalf("still-too-large candidate not queued: %+v", l.pending)
	}
}

func TestPendingDrainedOnLaterPass(t *testing.T) {
	mem := newBudgetMem(5)
	// First pass: a plain extractor with no consolidator → the big fact is queued.
	ex1 := &oneBig{body: "0123456789"}
	l := NewLearner(mem, "s", mustScope(), ex1, "", 0)
	if err := l.Consolidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(l.pending) != 1 {
		t.Fatalf("setup: expected 1 queued, got %d", len(l.pending))
	}
	// Swap in an extractor+consolidator and re-run: the drain resolves the queued
	// candidate. The new extractor returns nothing new so only the drain persists.
	l.extract = &shrinkingExtractor{oneBig: oneBig{body: "unused"}, to: "ok"}
	// Neutralise the new extractor's own output so the test isolates the drain:
	// oneBig.body "unused" is 6 runes (> 5) and would re-queue under key facts/big,
	// deduping against the drained one — so assert on persistence, not queue size.
	if err := l.Consolidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n, ok := mem.nodes["facts/big"]; !ok || n.Body != "ok" {
		t.Fatalf("queued candidate not resolved by drain on later pass: %+v ok=%v", mem.nodes["facts/big"], ok)
	}
}

func TestQueueDedupsByKey(t *testing.T) {
	mem := newBudgetMem(5)
	ex := &oneBig{body: "0123456789"} // same over-budget key every pass, no consolidator
	l := NewLearner(mem, "s", mustScope(), ex, "", 0)
	for i := 0; i < 3; i++ {
		if err := l.Consolidate(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(l.pending) != 1 {
		t.Fatalf("queue must dedup by key across passes: %d", len(l.pending))
	}
}
```

Note on `TestPendingDrainedOnLaterPass`: the drain runs *before* the new extractor's own output, and the consolidator shrinks whatever over-budget `facts/big` it sees to `"ok"`, so both the drained candidate and any re-extracted one resolve to `"ok"` — the assertion is on the persisted body, robust to ordering.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test -run 'TestPersist|TestPending|TestQueueDedups' ./...`
Expected: compile error — `l.pending` field does not exist yet, and no `Consolidator` handling. (A compile failure is an acceptable red: the symbols are introduced in Step 3.)

- [ ] **Step 3: Implement the seam, helpers, queue, and rewired Consolidate**

Update `learner.go` imports to:
```go
import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"unicode/utf8"

	"github.com/Herrscherd/herrscher-contracts"
)
```

Add the `Consolidator` seam next to `Extractor` (after `learner.go:26`):
```go
// Consolidator shrinks an over-budget memory candidate so a refused Record can be
// retried instead of dropped (memory roadmap G1). The merge/summarise heuristics
// are the closed part of the moat; this package defines only the seam and the
// open plumbing (Learner) that drives it. The returned node's Body SHOULD fit the
// given rune limit; the Learner re-checks and, if it still does not, holds the
// candidate for a later pass.
type Consolidator interface {
	Consolidate(ctx context.Context, over contracts.Node, limit int) (contracts.Node, error)
}

// errEnqueue signals that a candidate was refused for budget and could not be
// resolved this pass; the caller holds it on the pending queue. It never escapes
// Consolidate.
var errEnqueue = errors.New("orchestrator: candidate over budget, queued for retry")
```

Add the `pending` field to the `Learner` struct (after the `seen` field, `learner.go:45`):
```go
	// pending holds candidates refused for the per-node budget that could not be
	// consolidated to fit this pass. It is drained at the top of each Consolidate
	// (a consolidator may now be wired, or the budget may have changed) and is
	// in-memory only — the raw journal on disk remains the durable source, so a
	// chronically-unmergeable fact is simply retried for the life of the session.
	pending []Candidate
```

Add the helper methods (place them just after `Consolidate`):
```go
// consolidator returns the Consolidator the extractor also implements, if any.
// The closed extractor typically implements both seams, so forced consolidation
// needs no new constructor parameter.
func (l *Learner) consolidator() (Consolidator, bool) {
	c, ok := l.extract.(Consolidator)
	return c, ok
}

// record writes one candidate under the scope chosen by c.Private.
func (l *Learner) record(ctx context.Context, c Candidate) error {
	if c.Private {
		return contracts.RecordPrivate(ctx, l.mem, l.scope, c.Node)
	}
	return contracts.RecordShared(ctx, l.mem, l.scope, c.Node)
}

// persist records one candidate, responding to a per-node budget refusal
// (*contracts.BudgetError) by asking the Consolidator, if wired, to shrink the
// node to the refusal's Limit and retrying the write once. Returns nil on
// success, errEnqueue when a budget refusal is unresolved this pass (caller
// queues), or the underlying error for a non-budget failure (caller records it
// and keeps going).
func (l *Learner) persist(ctx context.Context, c Candidate) error {
	err := l.record(ctx, c)
	if err == nil {
		return nil
	}
	var be *contracts.BudgetError
	if !errors.As(err, &be) {
		return err // non-budget failure: caller keeps going
	}
	cons, ok := l.consolidator()
	if !ok {
		slog.Warn("memory: candidate over budget and no consolidator; queued for retry",
			"key", c.Node.Key, "runes", be.Runes, "limit", be.Limit)
		return errEnqueue
	}
	merged, cerr := cons.Consolidate(ctx, c.Node, be.Limit)
	if cerr != nil || utf8.RuneCountInString(merged.Body) > be.Limit {
		slog.Warn("memory: consolidation did not bring candidate within budget; queued for retry",
			"key", c.Node.Key, "runes", be.Runes, "limit", be.Limit, "err", cerr)
		return errEnqueue
	}
	c.Node = merged
	rerr := l.record(ctx, c)
	if rerr == nil {
		return nil
	}
	if errors.As(rerr, &be) {
		// Still refused after shrinking — hold it rather than loop.
		slog.Warn("memory: consolidated candidate still over budget; queued for retry",
			"key", c.Node.Key, "runes", be.Runes, "limit", be.Limit)
		return errEnqueue
	}
	return rerr // a non-budget failure on retry surfaces as a first-error
}

// enqueue holds a budget-refused candidate for a later pass, deduped by node key
// so a chronically-unmergeable fact cannot grow the queue unbounded.
func (l *Learner) enqueue(c Candidate) {
	for _, p := range l.pending {
		if p.Node.Key == c.Node.Key {
			return
		}
	}
	l.pending = append(l.pending, c)
}

// drain re-attempts each pending candidate through persist, returning those still
// refused. A now-successful candidate is marked seen; a non-budget failure is
// recorded in *firstErr and the candidate dropped (it re-extracts from the
// journal if still relevant).
func (l *Learner) drain(ctx context.Context, firstErr *error) {
	var still []Candidate
	for _, c := range l.pending {
		switch perr := l.persist(ctx, c); {
		case perr == nil:
			l.seen[c.Node.Key] = true
		case errors.Is(perr, errEnqueue):
			still = append(still, c)
		default:
			if *firstErr == nil {
				*firstErr = perr
			}
		}
	}
	l.pending = still
}
```

Rewrite the `Consolidate` loop from Task 1 to route through `persist` and drain the queue first. Replace the body after the nil-guard so it reads:
```go
	var firstErr error
	// Drain candidates a prior pass refused for budget before taking new ones: a
	// consolidator may now be wired, or the budget may have changed.
	l.drain(ctx, &firstErr)

	journal := l.readJournalTail() // best-effort: missing file / no new bytes → ""
	var transcript string
	if sg, err := l.mem.Recall(ctx, l.session, 0); err == nil {
		transcript = sg.Root.Body
	}
	cands, err := l.extract.Extract(ctx, journal, transcript)
	if err != nil {
		return err
	}
	for _, c := range cands {
		if l.seen[c.Node.Key] {
			continue // already persisted this session — keep Consolidate idempotent
		}
		switch perr := l.persist(ctx, c); {
		case perr == nil:
			l.seen[c.Node.Key] = true
		case errors.Is(perr, errEnqueue):
			l.enqueue(c)
		default:
			if firstErr == nil {
				firstErr = perr // record the first failure and keep going: one bad
			} // candidate must not drop its siblings or skip the sweep below
		}
	}
	// Best-effort staleness sweep at the end of a consolidation pass. A sweep
	// error must never propagate out of Consolidate (invariant: learning never
	// breaks a turn).
	_ = l.Sweep(ctx)
	return firstErr
```

- [ ] **Step 4: Run the full suite**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... && gofmt -l learner.go learner_budget_test.go`
Expected: all tests PASS (new budget tests + Task 1 test + all pre-existing tests, including `TestConsolidateContinuesPastRecordError`, which now routes through `persist`); `gofmt -l` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
git add learner.go learner_budget_test.go
git commit -m "feat(learner): forced consolidation — Consolidator seam + budget-catch retry queue"
```

---

## Release Wiring (main-agent, after both tasks reviewed)

Not a subagent task — the controller runs this (git tag/push/network ops).

- [ ] Verify against real published tags: `cd /home/shan/dev/herrscher-orchestrator && GOWORK=off go build ./... && GOWORK=off go test ./...` (orchestrator depends only on the already-released contracts v0.2.9).
- [ ] Tag + push: `git tag herrscher-orchestrator? ` → the orchestrator module tag is `v0.1.10` (confirm the module's tag convention from `git tag --list 'v0.1.*'` before pushing; G3 pushed `v0.1.9`).
- [ ] Host `go.mod`: bump `github.com/Herrscherd/herrscher-orchestrator v0.1.9` → `v0.1.10` (contracts/obsidian untouched). `GOWORK=off go build ./... && GOWORK=off go test ./...` GREEN.
- [ ] Update the host `README.md` "Learning (the write side)" section with a sentence on forced consolidation (budget refusal → consolidate-to-fit → retry queue), and the memory-vs-hermes memory (G1 SHIPPED).

## Documentation to check

- Spec: `docs/superpowers/specs/2026-07-30-g1-forced-consolidation-design.md`
- Umbrella roadmap: `docs/superpowers/specs/2026-07-30-memory-learning-hermes-parity-design.md`
- Host `README.md` — "### Learning (the write side)" subsection.
