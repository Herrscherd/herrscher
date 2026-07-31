# G4 Reversible Archive Audit Trail (REPORT node) + `memory restore` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every `Consolidate` pass (G1 budget, G2 merge, G3 staleness) an audit trail — a per-pass `Transition` log rendered into a `REPORT` node — and give an operator an explicit, guarded way to undo an archival via a `Restore` free function and a new `memory restore` host CLI verb.

**Architecture:** `herrscher-orchestrator` gains a `Transition` type accumulated on `*Curator` (embedded into `*Learner`) by `Sweep` (after a successful per-node `Record`) and by `Learner.Merge`'s `applyUmbrella` (after the umbrella `Record` and each successful original archive `Record`). A new `Restore` free function (not a `Curator`/`Learner` method, not a new `contracts.Memory` capability) reactivates a node over the existing `Recall`+`Record` port, refusing a merged-into original unless `Force(true)`; `*Learner.Restore` is a thin wrapper that also logs a `Transition`. `Learner.Consolidate`'s existing best-effort tail gains `_ = l.report(ctx)` (writes a markdown `REPORT` node, one per run, keyed `reportPrefix + timestamp`) followed by an unconditional `l.transitions = nil` reset. `register.go` wires a `report-enabled`/`report-prefix` config triple via `SetReport`, mirroring `SetStaleness`/`SetMerge`. The `herrscher` host adds one CLI verb, `memory restore`, mirroring `memory forget`, and a non-blank `orchestrator` import in `core/host/cli.go`. No `herrscher-contracts` or `herrscher-obsidian-memory` change.

**Tech Stack:** Go. `herrscher-orchestrator` module `github.com/Herrscherd/herrscher-orchestrator` (dep: already-released `github.com/Herrscherd/herrscher-contracts` v0.2.9). `herrscher` host module `github.com/Herrscherd/herrscher`, dev-only `go.work` overlay to build against the local orchestrator checkout. Tests use in-package fakes (`go test ./...`).

## Global Constraints

- **Scope:** `herrscher-orchestrator` (release → **v0.1.13**) + `herrscher` host (bump orchestrator v0.1.12→v0.1.13, new `memory restore` CLI verb). **No** `herrscher-contracts` change (depends on already-released contracts v0.2.9). **No** `herrscher-obsidian-memory` change.
- **Release wiring is OUT of this plan's scope** (done by the main agent, after review): tag orchestrator v0.1.13, bump host `go.mod`, `GOWORK=off` build/test. The orchestrator repo is used at master (commit/tag/push directly, it is a separate repo); the host is a harness-owned worktree (PR to master, never checkout/reset the live branch).
- **Dev workspace:** a `go.work` overlay at the host root makes the host build against the local orchestrator checkout HEAD during implementation. Task 0 creates it (untracked — verify via `git status --short` showing `?? go.work`).
- **Three invariants (verbatim from the spec), hold across every task:**
  1. **Ports only, policy not engine** — `Transition`/report types and the `Restore` free function are built over the existing `contracts.Memory` port (`Recall`+`Record`); no new `contracts.Memory` capability interface, no `contracts`/obsidian change, no file-layout knowledge in the orchestrator.
  2. **Learning never breaks a turn** — the report write is appended to `Consolidate`'s existing best-effort tail (`_ = l.report(ctx)`), folded into `firstErr` exactly like `Sweep`/`Merge`; `Restore` itself is an explicit, out-of-band operator action (CLI verb or direct call), not on the turn path, so it is allowed to return a real error to its caller.
  3. **Reversible over destructive** — G4 does not touch G3's label-based reversibility; it adds visibility (the report) and an explicit undo (`Restore`) on top of it. `Restore` defaults to refusing to silently resurrect a folded merge fragment (`ErrMergedOriginal`), requiring an explicit `Force` — reversal is available but never accidental.
- **Best-effort report tail:** `_ = l.report(ctx)` is folded into `firstErr` like `Sweep`/`Merge`; `l.transitions = nil` resets unconditionally at the end of every `Consolidate` pass, win or lose.
- **Report shape:** a `contracts.Node` with orchestrator-local `const ReportKind contracts.NodeKind = "report"`; a new key per run, `reportPrefix + now.UTC().Format(time.RFC3339)`; skipped (no write) when disabled or zero transitions this pass.
- **Restore shape:** a FREE function `orchestrator.Restore(ctx, mem, key, opts ...RestoreOption) error` + `Force(bool) RestoreOption` + `var ErrMergedOriginal error`. Refuses a merged original (`Meta[MetaMergedInto] != ""`) unless `Force(true)`. Clears `Meta[MetaState]` to active and refreshes `Meta[MetaLastSeen]` to now. An absent key is a REAL error (unlike `Deleter.Delete`'s idempotent-on-absent contract). Plus a thin `*Learner.Restore` wrapper that appends a `Transition{Kind:"restore"}`.
- **Config triple:** `report-enabled`/`MEMORY_REPORT_ENABLED` (default true; `"false"`/`"0"`/`"off"` disable) + `report-prefix`/`MEMORY_REPORT_PREFIX` (default `"reports/"`, empty → default), wired through `func (l *Learner) SetReport(enabled bool, prefix string)`.
- Run `gofmt -l` and `go vet ./...` clean before every commit, in both repos.

---

## File Structure

**`herrscher-orchestrator` (dev checkout: `/home/shan/dev/herrscher-orchestrator`):**
- **Modify `orchestrator.go`** — add `type Transition struct{...}` and a `transitions []Transition` field on `Curator`.
- **Modify `sweep.go`** — append a `Transition{Kind:"sweep"}` after a successful per-node `Record`.
- **Modify `merge.go`** — `applyUmbrella` appends one `Transition{Kind:"merge", To:"active"}` for the umbrella and one `Transition{Kind:"merge", To:"archived"}` per successfully-archived original.
- **Create `restore.go`** — `ErrMergedOriginal`, `RestoreOption`, `Force`, `withClock` (test-only clock override), `Restore` free function, `func (l *Learner) Restore(...)`.
- **Create `report.go`** — `ReportKind`, `defaultReportPrefix`, `func (l *Learner) SetReport(...)`, `func (l *Learner) report(ctx) error`, `renderReport(...)`.
- **Modify `learner.go`** — add `reportEnabled bool` / `reportPrefix string` fields; wire `_ = l.report(ctx)` + `l.transitions = nil` into `Consolidate`'s tail.
- **Modify `register.go`** — two new `Setting`s (`report-enabled`, `report-prefix`) + factory wiring.
- **Modify `sweep_test.go`** — 3 new transition-bookkeeping tests.
- **Modify `merge_test.go`** — 2 new transition-bookkeeping tests.
- **Create `restore_test.go`** — new `restoreMem` fake + `Restore`/`Learner.Restore` tests.
- **Create `report_test.go`** — `report`/`SetReport`/Consolidate-reset tests (reuses `mergeMem` and `plainExt` from `merge_test.go`).

**`herrscher` host (this worktree):**
- **Modify `core/host/cli.go`** — add non-blank `orchestrator "github.com/Herrscherd/herrscher-orchestrator"` import + `memory restore` command.
- **Create `core/host/memory_restore_verb_test.go`** — end-to-end CLI-dispatch test over a fake `contracts.Memory` registered as the first memory plugin.
- **Create (untracked) `go.work`** at the host root — dev-only workspace overlay (Task 0).

---

### Task 0: `go.work` dev overlay (main-agent setup, not TDD)

**Files:**
- Create (untracked): `go.work` at the host root (`/home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review/go.work`)

**Interfaces:**
- Consumes: nothing.
- Produces: a local build/test environment where the host resolves `github.com/Herrscherd/herrscher-orchestrator` to the local checkout at `/home/shan/dev/herrscher-orchestrator` HEAD, regardless of the version pinned in `go.mod` (v0.1.12). Every later task's `go build`/`go test` in the host repo relies on this.

- [ ] **Step 1: Initialize the workspace over the host module**

```bash
cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review
go work init .
```

- [ ] **Step 2: Add the local orchestrator checkout to the workspace**

```bash
go work use /home/shan/dev/herrscher-orchestrator
```

- [ ] **Step 3: Verify the overlay is untracked and the workspace resolves**

```bash
git status --short | grep go.work
```

Expected: `?? go.work` (and possibly `?? go.work.sum` once a build populates it) — never staged, never committed.

```bash
go build ./... 2>&1 | tail -20
```

Expected: builds clean (the host's existing code is unaffected; this only proves the workspace overlay resolves both modules before any G4 code exists).

- [ ] **Step 4: No commit for this task**

`go.work`/`go.work.sum` are dev-local and untracked; nothing is committed. Proceed to Task 1 in the orchestrator checkout.

---

### Task 1: `Transition` type + `Curator.transitions` + Sweep bookkeeping

**Files:**
- Modify: `/home/shan/dev/herrscher-orchestrator/orchestrator.go`
- Modify: `/home/shan/dev/herrscher-orchestrator/sweep.go`
- Test: `/home/shan/dev/herrscher-orchestrator/sweep_test.go`

**Interfaces:**
- Consumes: `contracts.MetaState`, `contracts.StateActive`, `contracts.StateStale`, `contracts.StateArchived`, `contracts.MetaLastSeen`, `contracts.NextState` (all pre-existing).
- Produces: `type Transition struct{ Key, From, To, Kind string }`; a `transitions []Transition` field on `Curator` (promoted into `*Learner` via embedding). Task 2 appends more `Transition{Kind:"merge"}` values to the same slice; Task 3 appends `Transition{Kind:"restore"}`; Task 4 reads `l.transitions` in `report` and resets it to `nil`.

- [ ] **Step 1: Write the failing tests (append to `sweep_test.go`)**

```go
func TestSweepPopulatesTransitionOnChange(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	f := newSweepFakeMem()
	seed(f, "old", 45, now) // 45 days > 30-day staleAfter, absent Meta[MetaState] == active
	c := NewScoped(f, "s", contracts.MemoryScope{})
	c.now = func() time.Time { return now }
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.transitions) != 1 {
		t.Fatalf("transitions = %d, want 1", len(c.transitions))
	}
	tr := c.transitions[0]
	if tr.Key != "old" || tr.From != contracts.StateActive || tr.To != contracts.StateStale || tr.Kind != "sweep" {
		t.Fatalf("unexpected transition: %+v", tr)
	}
}

func TestSweepNoTransitionWhenUnchanged(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	f := newSweepFakeMem()
	seed(f, "fresh", 1, now) // already active; must not churn
	c := NewScoped(f, "s", contracts.MemoryScope{})
	c.now = func() time.Time { return now }
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.transitions) != 0 {
		t.Fatalf("transitions = %d, want 0 for an unchanged node", len(c.transitions))
	}
}

func TestSweepWriteFailureProducesNoTransition(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	f := newSweepFakeMem()
	seed(f, "bad", 45, now) // would transition -> stale, but its write fails
	f.failOn = "bad"
	c := NewScoped(f, "s", contracts.MemoryScope{})
	c.now = func() time.Time { return now }
	if err := c.Sweep(context.Background()); err == nil {
		t.Fatal("expected the failing node's error to be returned")
	}
	if len(c.transitions) != 0 {
		t.Fatalf("transitions = %d, want 0: a failed write must not claim a transition", len(c.transitions))
	}
}
```

- [ ] **Step 2: Run to verify FAIL**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestSweep -v`
Expected: compile error — `c.transitions` (and `Transition`) do not exist yet.

- [ ] **Step 3: Add `Transition` + the `transitions` field to `orchestrator.go`**

Add the type just above the `Curator` struct declaration:

```go
// Transition is one state change a curator pass made to one node, for the
// audit report. Kind is "sweep" (G3 staleness), "merge" (G2 umbrella-fold), or
// "restore" (an explicit reactivation).
type Transition struct {
	Key  string // node Key that changed
	From string // prior Meta[MetaState] ("" treated as StateActive)
	To   string // new Meta[MetaState]
	Kind string // "sweep" | "merge" | "restore"
}
```

Add the field to the `Curator` struct (after `archiveAfter time.Duration`):

```go
	// transitions is this Consolidate pass's audit trail: every state change
	// Sweep/Merge/Restore made. Reset to nil at the end of Consolidate (see
	// learner.go) regardless of outcome, so a report never accumulates
	// duplicate rows across passes.
	transitions []Transition
```

- [ ] **Step 4: Append the transition in `sweep.go` after a successful `Record`**

Replace the tail of the per-node loop in `Sweep` — currently:

```go
		// Best-effort: a per-node write failure (e.g. a node that trips the
		// obsidian budget) must not abort the rest of the sweep, or one bad
		// node would freeze decay for every node after it, every pass. Record
		// the first error and keep going; Consolidate swallows the return.
		if err := c.mem.Record(ctx, n); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
```

with:

```go
		// Best-effort: a per-node write failure (e.g. a node that trips the
		// obsidian budget) must not abort the rest of the sweep, or one bad
		// node would freeze decay for every node after it, every pass. Record
		// the first error and keep going; Consolidate swallows the return.
		if err := c.mem.Record(ctx, n); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue // do not record a transition for a write that didn't land
		}
		c.transitions = append(c.transitions, Transition{Key: n.Key, From: cur, To: next, Kind: "sweep"})
	}
	return firstErr
}
```

- [ ] **Step 5: Run to verify PASS**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestSweep -v`
Expected: all `TestSweep*` PASS.
Run: `go test ./...` → PASS.

- [ ] **Step 6: gofmt + vet + commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
gofmt -w orchestrator.go sweep.go sweep_test.go
go vet ./...
git add orchestrator.go sweep.go sweep_test.go
git commit -m "feat(sweep): record a Transition audit trail for staleness state changes"
```

---

### Task 2: Merge appends `Transition`s in `applyUmbrella`

**Files:**
- Modify: `/home/shan/dev/herrscher-orchestrator/merge.go`
- Test: `/home/shan/dev/herrscher-orchestrator/merge_test.go`

**Interfaces:**
- Consumes: `Transition` (Task 1); `Learner.transitions` (Task 1, promoted from `Curator`); `MetaMergedInto`, `Umbrella`, `l.validUmbrella` (pre-existing).
- Produces: nothing new — `applyUmbrella` still returns `error` only; it now also grows `l.transitions`. Task 4's `report` reads whatever `l.transitions` holds after `Merge` runs.

- [ ] **Step 1: Write the failing tests (append to `merge_test.go`)**

```go
func TestMergePopulatesTransitions(t *testing.T) {
	a, b := stale("facts/a", "d"), stale("facts/b", "d")
	mem := &mergeMem{nodes: []contracts.Node{a, b}}
	f := &fakeMerger{result: []Umbrella{{
		Node:   contracts.Node{Key: "facts/umbrella", Body: "fused"},
		Merged: []string{"facts/a", "facts/b"},
	}}}
	l := learnerWith(mem, f, 2, 40, "stale")
	if err := l.Merge(context.Background()); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(l.transitions) != 3 {
		t.Fatalf("transitions = %d, want 3 (1 umbrella + 2 originals): %+v", len(l.transitions), l.transitions)
	}
	var sawUmbrella, sawA, sawB bool
	for _, tr := range l.transitions {
		if tr.Kind != "merge" {
			t.Errorf("transition kind = %q, want merge", tr.Kind)
		}
		switch tr.Key {
		case "facts/umbrella":
			sawUmbrella = tr.From == "" && tr.To == contracts.StateActive
		case "facts/a":
			sawA = tr.From == contracts.StateStale && tr.To == contracts.StateArchived
		case "facts/b":
			sawB = tr.From == contracts.StateStale && tr.To == contracts.StateArchived
		}
	}
	if !sawUmbrella || !sawA || !sawB {
		t.Fatalf("missing expected transition(s): umbrella=%v a=%v b=%v (%+v)", sawUmbrella, sawA, sawB, l.transitions)
	}
}

func TestMergeTransitionSkippedOnOriginalRecordError(t *testing.T) {
	mem := &mergeMem{
		nodes:    []contracts.Node{stale("facts/a", "d"), stale("facts/b", "d")},
		recErrOn: "facts/a", // archiving facts/a fails
	}
	f := &fakeMerger{result: []Umbrella{{
		Node:   contracts.Node{Key: "facts/u", Body: "fused"},
		Merged: []string{"facts/a", "facts/b"},
	}}}
	l := learnerWith(mem, f, 2, 40, "stale")
	_ = l.Merge(context.Background()) // error surfaced, but the sibling still applies
	for _, tr := range l.transitions {
		if tr.Key == "facts/a" {
			t.Fatalf("facts/a must not produce a transition: its archive Record failed")
		}
	}
	if len(l.transitions) != 2 {
		t.Fatalf("transitions = %d, want 2 (umbrella + facts/b): %+v", len(l.transitions), l.transitions)
	}
}
```

- [ ] **Step 2: Run to verify FAIL**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestMergePopulatesTransitions -v`
Expected: FAIL — `l.transitions` stays empty (nothing appends to it yet).

- [ ] **Step 3: Update `applyUmbrella` in `merge.go`**

Replace the current body of `applyUmbrella`:

```go
func (l *Learner) applyUmbrella(ctx context.Context, u Umbrella, group []contracts.Node, existing map[string]bool) error {
	byKey := make(map[string]contracts.Node, len(group))
	for _, n := range group {
		byKey[n.Key] = n
	}
	if !l.validUmbrella(u, byKey, existing) {
		return nil // rejected (WARN already emitted)
	}
	if err := l.mem.Record(ctx, u.Node); err != nil {
		return err
	}
	// The umbrella is a brand-new node (validUmbrella rejects a key collision),
	// so its prior state is unconditionally "none" -> it now exists active.
	l.transitions = append(l.transitions, Transition{Key: u.Node.Key, From: "", To: contracts.StateActive, Kind: "merge"})
	var firstErr error
	for _, k := range u.Merged {
		orig := byKey[k]
		prevState := orig.Meta[contracts.MetaState]
		if prevState == "" {
			prevState = contracts.StateActive
		}
		if orig.Meta == nil {
			orig.Meta = map[string]string{}
		}
		orig.Meta[MetaMergedInto] = u.Node.Key
		orig.Meta[contracts.MetaState] = contracts.StateArchived
		// orig already carries its lastSeen from Search; re-recording with it
		// present keeps obsidian's per-write stamp from bumping the age (same
		// contract Sweep relies on).
		if err := l.mem.Record(ctx, orig); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue // do not record a transition for a write that didn't land
		}
		l.transitions = append(l.transitions, Transition{Key: k, From: prevState, To: contracts.StateArchived, Kind: "merge"})
		if err := l.mem.Links(ctx, k, u.Node.Key, "merged-into"); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
```

- [ ] **Step 4: Run to verify PASS + full suite**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestMerge -v` → all `TestMerge*` PASS.
Run: `go test ./...` → PASS.

- [ ] **Step 5: gofmt + vet + commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
gofmt -w merge.go merge_test.go
go vet ./...
git add merge.go merge_test.go
git commit -m "feat(merge): record a Transition audit trail for umbrella folds"
```

---

### Task 3: `Restore` free function + `Force` + `ErrMergedOriginal` + `Learner.Restore`

**Files:**
- Create: `/home/shan/dev/herrscher-orchestrator/restore.go`
- Create: `/home/shan/dev/herrscher-orchestrator/restore_test.go`

**Interfaces:**
- Consumes: `contracts.Memory` (`Recall`, `Record`), `contracts.MetaState`, `contracts.StateActive`, `contracts.MetaLastSeen` (pre-existing); `MetaMergedInto` (Task 1's predecessor, defined in `merge.go`); `Transition` (Task 1); `Learner.mem`, `Learner.transitions` (embedded `*Curator`); `plainExt` (test helper defined in `merge_test.go`, same package).
- Produces: `var ErrMergedOriginal error`; `type RestoreOption func(*restoreConfig)`; `func Force(force bool) RestoreOption`; `func Restore(ctx context.Context, mem contracts.Memory, key string, opts ...RestoreOption) error`; `func (l *Learner) Restore(ctx context.Context, key string, opts ...RestoreOption) error`. Task 6 (host CLI) consumes `orchestrator.Restore` and `orchestrator.Force` verbatim; Task 4/5 do not depend on this task.

- [ ] **Step 1: Write the failing tests in a new `restore_test.go`**

```go
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts"
)

// restoreMem is a minimal fake Memory dedicated to Restore's tests: unlike
// mergeMem/sweepFakeMem (which return a zero Node with no error on a missing
// key), Recall here models the real contract Restore relies on — an absent key
// is a genuine error, not a silent zero value.
type restoreMem struct {
	nodes   map[string]contracts.Node
	records []contracts.Node
}

func newRestoreMem() *restoreMem { return &restoreMem{nodes: map[string]contracts.Node{}} }

func (m *restoreMem) Recall(_ context.Context, key string, _ int) (contracts.Subgraph, error) {
	n, ok := m.nodes[key]
	if !ok {
		return contracts.Subgraph{}, fmt.Errorf("restoreMem: no node at key %q", key)
	}
	return contracts.Subgraph{Root: n}, nil
}
func (m *restoreMem) Record(_ context.Context, n contracts.Node) error {
	m.nodes[n.Key] = n
	m.records = append(m.records, n)
	return nil
}
func (m *restoreMem) Search(context.Context, contracts.Query) ([]contracts.Node, error) {
	return nil, nil
}
func (m *restoreMem) Links(context.Context, string, string, string) error { return nil }
func (m *restoreMem) Close() error                                       { return nil }

func TestRestoreHappyPath(t *testing.T) {
	m := newRestoreMem()
	m.nodes["facts/a"] = contracts.Node{Key: "facts/a", Meta: map[string]string{
		contracts.MetaState:    contracts.StateArchived,
		contracts.MetaLastSeen: "2020-01-01T00:00:00Z",
	}}
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	if err := Restore(context.Background(), m, "facts/a", withClock(func() time.Time { return now })); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got := m.nodes["facts/a"]
	if got.Meta[contracts.MetaState] != contracts.StateActive {
		t.Errorf("state = %q, want active", got.Meta[contracts.MetaState])
	}
	if want := now.UTC().Format(time.RFC3339); got.Meta[contracts.MetaLastSeen] != want {
		t.Errorf("lastSeen = %q, want %q", got.Meta[contracts.MetaLastSeen], want)
	}
}

func TestRestoreRefusesMergedOriginal(t *testing.T) {
	m := newRestoreMem()
	m.nodes["facts/a"] = contracts.Node{Key: "facts/a", Meta: map[string]string{
		MetaMergedInto:         "facts/u",
		contracts.MetaState:    contracts.StateArchived,
		contracts.MetaLastSeen: "2020-01-01T00:00:00Z",
	}}
	err := Restore(context.Background(), m, "facts/a")
	if !errors.Is(err, ErrMergedOriginal) {
		t.Fatalf("err = %v, want ErrMergedOriginal", err)
	}
	if len(m.records) != 0 {
		t.Fatalf("a refused restore must not write; got %d records", len(m.records))
	}
	got := m.nodes["facts/a"]
	if got.Meta[contracts.MetaState] != contracts.StateArchived {
		t.Errorf("node was mutated despite refusal: state=%q", got.Meta[contracts.MetaState])
	}
}

func TestRestoreWithForceDetaches(t *testing.T) {
	m := newRestoreMem()
	m.nodes["facts/a"] = contracts.Node{Key: "facts/a", Meta: map[string]string{
		MetaMergedInto:         "facts/u",
		contracts.MetaState:    contracts.StateArchived,
		contracts.MetaLastSeen: "2020-01-01T00:00:00Z",
	}}
	if err := Restore(context.Background(), m, "facts/a", Force(true)); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got := m.nodes["facts/a"]
	if got.Meta[contracts.MetaState] != contracts.StateActive {
		t.Errorf("state = %q, want active", got.Meta[contracts.MetaState])
	}
	if got.Meta[MetaMergedInto] != "" {
		t.Errorf("mergedInto = %q, want cleared", got.Meta[MetaMergedInto])
	}
}

func TestRestoreAbsentKeyErrors(t *testing.T) {
	m := newRestoreMem()
	if err := Restore(context.Background(), m, "nope"); err == nil {
		t.Fatal("expected an error restoring an absent key")
	}
	if len(m.records) != 0 {
		t.Fatalf("an absent-key restore must not write; got %d records", len(m.records))
	}
}

func TestLearnerRestoreAppendsTransition(t *testing.T) {
	m := newRestoreMem()
	m.nodes["facts/a"] = contracts.Node{Key: "facts/a", Meta: map[string]string{
		contracts.MetaState:    contracts.StateArchived,
		contracts.MetaLastSeen: "2020-01-01T00:00:00Z",
	}}
	l := NewLearner(m, "s", contracts.MemoryScope{}, plainExt{}, "", 0)
	if err := l.Restore(context.Background(), "facts/a"); err != nil {
		t.Fatalf("Learner.Restore: %v", err)
	}
	if len(l.transitions) != 1 {
		t.Fatalf("transitions = %d, want 1", len(l.transitions))
	}
	tr := l.transitions[0]
	if tr.Key != "facts/a" || tr.Kind != "restore" || tr.To != contracts.StateActive {
		t.Fatalf("unexpected transition: %+v", tr)
	}
}
```

- [ ] **Step 2: Run to verify FAIL**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestRestore -v`
Expected: compile error — `Restore`, `Force`, `ErrMergedOriginal`, `withClock`, `Learner.Restore` do not exist yet.

- [ ] **Step 3: Write `restore.go`**

```go
package orchestrator

import (
	"context"
	"errors"
	"time"

	"github.com/Herrscherd/herrscher-contracts"
)

// ErrMergedOriginal is returned by Restore when key names a node folded into an
// umbrella (Meta[MetaMergedInto] != "") and the caller did not pass Force(true).
// Restoring a merged fragment without detaching it would resurrect a fragment
// the merge pass deliberately subsumed, while Sweep's mergedInto guard would
// immediately re-skip it and a later Merge pass could re-fold it right back.
var ErrMergedOriginal = errors.New("orchestrator: node is folded into an umbrella; restore with Force to detach it")

// restoreConfig holds the options one Restore call was given.
type restoreConfig struct {
	force bool
	clock func() time.Time
}

// RestoreOption configures one Restore call.
type RestoreOption func(*restoreConfig)

// Force, when true, allows restoring a merged-into original by also clearing
// Meta[MetaMergedInto] so the node becomes independent again. The umbrella
// node itself is untouched — it may now have one fewer live member, which is
// fine; umbrellas are additive, not authoritative.
func Force(force bool) RestoreOption {
	return func(c *restoreConfig) { c.force = force }
}

// withClock overrides Restore's injected clock. Unexported: it exists only so
// this package's own tests can assert on a deterministic Meta[MetaLastSeen]
// stamp; every real caller gets time.Now.
func withClock(clock func() time.Time) RestoreOption {
	return func(c *restoreConfig) { c.clock = clock }
}

// Restore reactivates an archived (or merged-into) node at key: it clears
// Meta[MetaState] to active and refreshes Meta[MetaLastSeen] to now, so the
// very next Sweep does not immediately re-derive it back to stale/archived
// from a stale timestamp. Idempotent: restoring an already-active node is a
// no-op write that still refreshes lastSeen.
//
// A node still carrying Meta[MetaMergedInto] is a folded fragment, not an
// independent archived node — restoring it without also detaching it from its
// umbrella would resurrect a fragment the merge pass deliberately subsumed.
// Restore therefore REFUSES a merged original by default: it returns
// ErrMergedOriginal unless the caller passes Force(true), in which case it
// also clears Meta[MetaMergedInto].
//
// This is a free function taking contracts.Memory, not a *Curator/*Learner
// method: it needs no orchestrator state beyond the injected clock, and the
// host's `memory restore` CLI verb builds a bare contracts.Memory with no
// session/scope, the same shape as the existing memory forget/record verbs.
//
// An error from Recall, including "not found", surfaces unchanged — restoring
// an absent key is a real error (nothing to reactivate), unlike Deleter.Delete's
// intentional idempotent-on-absent contract.
func Restore(ctx context.Context, mem contracts.Memory, key string, opts ...RestoreOption) error {
	cfg := restoreConfig{clock: time.Now}
	for _, o := range opts {
		o(&cfg)
	}
	sg, err := mem.Recall(ctx, key, 0)
	if err != nil {
		return err
	}
	n := sg.Root
	if n.Meta[MetaMergedInto] != "" && !cfg.force {
		return ErrMergedOriginal
	}
	if n.Meta == nil {
		n.Meta = map[string]string{}
	}
	n.Meta[contracts.MetaState] = contracts.StateActive
	n.Meta[contracts.MetaLastSeen] = cfg.clock().UTC().Format(time.RFC3339)
	if cfg.force {
		delete(n.Meta, MetaMergedInto)
	}
	return mem.Record(ctx, n)
}

// Restore reactivates key over l's Memory and, on success, appends a
// Transition{Kind:"restore"} to the pass's audit trail — surfaced in the next
// report if Consolidate runs before the field resets, or inspectable directly
// via l.transitions since Restore is typically called out of band between
// passes.
func (l *Learner) Restore(ctx context.Context, key string, opts ...RestoreOption) error {
	err := Restore(ctx, l.mem, key, opts...)
	if err == nil {
		l.transitions = append(l.transitions, Transition{Key: key, From: contracts.StateArchived, To: contracts.StateActive, Kind: "restore"})
	}
	return err
}
```

- [ ] **Step 4: Run to verify PASS + full suite**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestRestore -v` → all PASS.
Run: `go test ./... -run TestLearnerRestoreAppendsTransition -v` → PASS.
Run: `go test ./...` → PASS.

- [ ] **Step 5: gofmt + vet + commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
gofmt -w restore.go restore_test.go
go vet ./...
git add restore.go restore_test.go
git commit -m "feat(restore): add Restore free function + Force + ErrMergedOriginal + Learner.Restore"
```

---

### Task 4: `report` method + `ReportKind` + `SetReport` + Consolidate wiring + reset

**Files:**
- Create: `/home/shan/dev/herrscher-orchestrator/report.go`
- Create: `/home/shan/dev/herrscher-orchestrator/report_test.go`
- Modify: `/home/shan/dev/herrscher-orchestrator/learner.go`

**Interfaces:**
- Consumes: `Transition` (Task 1); `l.transitions` (grows via Tasks 1/2/3); `contracts.Node`, `contracts.NodeKind`, `mem.Record` (pre-existing); `mergeMem`/`plainExt` (test helpers from `merge_test.go`, same package).
- Produces: `const ReportKind contracts.NodeKind = "report"`; `const defaultReportPrefix = "reports/"`; `func (l *Learner) SetReport(enabled bool, prefix string)`; `func (l *Learner) report(ctx context.Context) error`; `reportEnabled bool` / `reportPrefix string` fields on `Learner`. Task 5 (`register.go`) consumes `SetReport` verbatim.

- [ ] **Step 1: Add the two fields to `Learner` in `learner.go`**

Append inside the `Learner` struct (after `mergeTarget string`, before the closing `}`):

```go
	// reportEnabled/reportPrefix configure the G4 audit-report pass
	// (Learner.report). Both are set via SetReport; register.go always calls
	// it (default enabled=true, prefix="reports/" — see the config table), so
	// an unconfigured host still gets a report.
	reportEnabled bool
	reportPrefix  string
```

- [ ] **Step 2: Wire `report` + the transitions reset into `Consolidate`'s tail**

Change the tail of `Consolidate` from:

```go
	// Best-effort semantic merge after the sweep (opt-in via SetMerge; a no-op
	// when disabled or no Merger is wired). A merge error must never propagate
	// out of Consolidate (invariant: learning never breaks a turn).
	_ = l.Merge(ctx)
	return firstErr
}
```

to:

```go
	// Best-effort semantic merge after the sweep (opt-in via SetMerge; a no-op
	// when disabled or no Merger is wired). A merge error must never propagate
	// out of Consolidate (invariant: learning never breaks a turn).
	_ = l.Merge(ctx)
	// Best-effort audit report of this pass's transitions (opt-in via
	// SetReport, default enabled; a no-op when disabled or when the pass made
	// no transitions). A report error must never propagate out of Consolidate
	// (invariant: learning never breaks a turn).
	_ = l.report(ctx)
	// Reset the pass-scoped audit trail regardless of write outcome, so the
	// next pass never re-reports this pass's transitions.
	l.transitions = nil
	return firstErr
}
```

- [ ] **Step 3: Write the failing tests in a new `report_test.go`**

```go
package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestReportSkippedWhenNoTransitions(t *testing.T) {
	mem := &mergeMem{}
	l := NewLearner(mem, "s", contracts.MemoryScope{}, plainExt{}, "", 0)
	l.SetReport(true, "")
	if err := l.report(context.Background()); err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(mem.records) != 0 {
		t.Fatalf("a quiet pass must write 0 reports; got %d", len(mem.records))
	}
}

func TestReportWrittenWithRightShape(t *testing.T) {
	mem := &mergeMem{}
	l := NewLearner(mem, "s", contracts.MemoryScope{}, plainExt{}, "", 0)
	l.SetReport(true, "reports/")
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	l.transitions = []Transition{
		{Key: "facts/a", From: contracts.StateActive, To: contracts.StateStale, Kind: "sweep"},
		{Key: "facts/b", From: contracts.StateStale, To: contracts.StateArchived, Kind: "merge"},
	}
	if err := l.report(context.Background()); err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(mem.records) != 1 {
		t.Fatalf("want exactly 1 report record, got %d", len(mem.records))
	}
	r := mem.records[0]
	if r.Kind != ReportKind {
		t.Errorf("kind = %q, want %q", r.Kind, ReportKind)
	}
	if !strings.HasPrefix(r.Key, "reports/") {
		t.Errorf("key %q does not start with the configured prefix", r.Key)
	}
	for _, key := range []string{"facts/a", "facts/b"} {
		if !strings.Contains(r.Body, key) {
			t.Errorf("body missing transitioned key %q:\n%s", key, r.Body)
		}
	}
}

func TestReportDisabledNoWrite(t *testing.T) {
	mem := &mergeMem{}
	l := NewLearner(mem, "s", contracts.MemoryScope{}, plainExt{}, "", 0)
	l.SetReport(false, "")
	l.transitions = []Transition{{Key: "facts/a", From: "", To: contracts.StateStale, Kind: "sweep"}}
	if err := l.report(context.Background()); err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(mem.records) != 0 {
		t.Fatalf("a disabled report must not write; got %d", len(mem.records))
	}
}

func TestSetReportDefaultPrefix(t *testing.T) {
	l := NewLearner(&mergeMem{}, "s", contracts.MemoryScope{}, plainExt{}, "", 0)
	l.SetReport(true, "")
	if l.reportPrefix != defaultReportPrefix {
		t.Errorf("reportPrefix = %q, want %q", l.reportPrefix, defaultReportPrefix)
	}
}

func TestConsolidateResetsTransitionsRegardless(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	mem := &mergeMem{nodes: []contracts.Node{
		{Key: "old", Meta: map[string]string{contracts.MetaLastSeen: now.Add(-45 * 24 * time.Hour).Format(time.RFC3339)}},
	}}
	l := NewLearner(mem, "s", contracts.MemoryScope{}, plainExt{}, "", 0)
	l.now = func() time.Time { return now }
	l.SetStaleness(30*24*time.Hour, 90*24*time.Hour)
	l.SetReport(true, "")
	if err := l.Consolidate(context.Background()); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}
	if l.transitions != nil {
		t.Fatalf("transitions not reset after Consolidate: %+v", l.transitions)
	}
	var sawReport bool
	for _, r := range mem.records {
		if r.Kind == ReportKind {
			sawReport = true
		}
	}
	if !sawReport {
		t.Fatal("expected a report node after a pass that made a sweep transition")
	}
}
```

- [ ] **Step 4: Run to verify FAIL**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestReport -v`
Expected: compile error — `report`, `ReportKind`, `SetReport`, `defaultReportPrefix` do not exist yet.

- [ ] **Step 5: Write `report.go`**

```go
package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher-contracts"
)

// ReportKind is the orchestrator-local Node.Kind used for a per-pass audit
// report. No contracts change: NodeKind is a plain string type and nothing in
// contracts/obsidian switches on Kind to gate behaviour — obsidian only
// compares it inside Query.Kinds filtering.
const ReportKind contracts.NodeKind = "report"

// defaultReportPrefix roots every report under one recallable key so `memory
// search --kind report` (existing generic Search) finds them; Sweep/Merge/
// Restore already exclude archived nodes from ordinary browsing, so reports
// living as ordinary nodes do not clutter Context/Recall (Context only ever
// recalls the session key + P1 scope roots, never an arbitrary Search).
const defaultReportPrefix = "reports/"

// SetReport configures G4 report emission. enabled=false is a clean no-op;
// prefix="" falls back to defaultReportPrefix.
func (l *Learner) SetReport(enabled bool, prefix string) {
	l.reportEnabled = enabled
	if prefix == "" {
		prefix = defaultReportPrefix
	}
	l.reportPrefix = prefix
}

// report writes one audit-trail node for the transitions this Consolidate
// pass made, if any. It is best-effort: disabled, a nil Memory, or a quiet
// pass with zero transitions all yield a clean no-op — the vault is never
// cluttered with empty reports. Each call writes a freshly-keyed node
// (reportPrefix + timestamp), never upserting a fixed key, so re-running
// Consolidate produces an append-only audit log rather than overwriting
// yesterday's report.
func (l *Learner) report(ctx context.Context) error {
	if !l.reportEnabled || len(l.transitions) == 0 || l.mem == nil {
		return nil
	}
	now := l.now().UTC()
	stamp := now.Format(time.RFC3339)
	return l.mem.Record(ctx, contracts.Node{
		Key:   l.reportPrefix + stamp,
		Kind:  ReportKind,
		Title: "consolidate report " + stamp,
		Body:  renderReport(stamp, l.transitions),
	})
}

// renderReport renders the markdown body: a one-line header with the pass
// timestamp and per-Kind counts, then one table row per Transition.
func renderReport(stamp string, transitions []Transition) string {
	counts := map[string]int{}
	for _, tr := range transitions {
		counts[tr.Kind]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# consolidate report %s\n\n", stamp)
	fmt.Fprintf(&b, "sweep=%d merge=%d restore=%d\n\n", counts["sweep"], counts["merge"], counts["restore"])
	b.WriteString("| key | kind | from | to |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, tr := range transitions {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", tr.Key, tr.Kind, tr.From, tr.To)
	}
	return b.String()
}
```

- [ ] **Step 6: Run to verify PASS + full suite**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestReport -v` → PASS.
Run: `go test ./... -run TestConsolidateResetsTransitionsRegardless -v` → PASS.
Run: `go test ./...` → PASS.

- [ ] **Step 7: gofmt + vet + commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
gofmt -w report.go report_test.go learner.go
go vet ./...
git add report.go report_test.go learner.go
git commit -m "feat(report): write a REPORT node per Consolidate pass with >=1 transition"
```

---

### Task 5: `register.go` config triple — `report-enabled` / `report-prefix`

**Files:**
- Modify: `/home/shan/dev/herrscher-orchestrator/register.go`

**Interfaces:**
- Consumes: `SetReport` (Task 4); `contracts.Setting`, `contracts.PluginConfig.Get` (pre-existing, same pattern as `SetStaleness`/`SetMerge`).
- Produces: two declared settings (`report-enabled`, `report-prefix`) reachable via `MEMORY_REPORT_ENABLED`/`MEMORY_REPORT_PREFIX` — no new Go symbol other than the manifest entries and the factory call.

- [ ] **Step 1: Add the two `Setting`s to the manifest**

In the `Config: []contracts.Setting{ ... }` block, after the `merge-max` line, add:

```go
				{Key: "report-enabled", Env: "MEMORY_REPORT_ENABLED", Help: "write a REPORT node at the end of a Consolidate pass that made >=1 transition; false/0/off disables (default true)", Required: false},
				{Key: "report-prefix", Env: "MEMORY_REPORT_PREFIX", Help: "key prefix each report node is written under, a timestamp is appended (default reports/)", Required: false},
```

- [ ] **Step 2: Wire `SetReport` in the Learner branch of the factory**

In the `if ex := lookupExtractor(...); ex != nil {` block, after `l.SetMerge(mergeMin, mergeMax, cfg.Get("merge-target"))` and before `return l, nil`, add:

```go
				reportEnabled := cfg.Get("report-enabled") != "false" && cfg.Get("report-enabled") != "0" && cfg.Get("report-enabled") != "off"
				l.SetReport(reportEnabled, cfg.Get("report-prefix"))
```

(`report-enabled` is absent by default → all three negative-string checks fail → `true`, matching the documented default with no separate "was it set" bookkeeping. No change to the plain-`Curator` branch: without an extractor there is no `Learner`, so there is nothing to report.)

- [ ] **Step 3: Run the full suite**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./...` → PASS (register.go still compiles and registers; existing plugin-registration coverage, if any, is untouched).

- [ ] **Step 4: gofmt + vet + commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
gofmt -w register.go
go vet ./...
git add register.go
git commit -m "feat(config): expose report-enabled/report-prefix settings"
```

---

### Task 6: Host `memory restore` CLI verb

**Files:**
- Modify: `/home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review/core/host/cli.go`
- Create: `/home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review/core/host/memory_restore_verb_test.go`

**Interfaces:**
- Consumes: `orchestrator.Restore(ctx, mem, key, opts...) error`, `orchestrator.Force(bool) orchestrator.RestoreOption`, `orchestrator.MetaMergedInto` (all Tasks 1/3, herrscher-orchestrator — resolved via the Task 0 `go.work` overlay to the local checkout); `BuildFirstMemory(ctx) (contracts.Memory, error)` (pre-existing, `core/host/memory.go`); `contracts.New/Help/Param/ValueParam/Do`, `contracts.Input.Get`; `NewRegistry(ctx, Deps, Options) (*cli.Registry, error)` (pre-existing, `core/host/cli.go`); `contracts.Register`, `contracts.Manifest{Kind, Category}`, `contracts.CategoryMemory`, `contracts.PluginConfig` (pre-existing).
- Produces: the `memory restore` command reachable via `reg.Dispatch(ctx, []string{"memory", "restore", "--key", k, "--force", "true"|"false"})`, returning `"restored " + key` on success.

- [ ] **Step 1: Write the failing tests in a new `memory_restore_verb_test.go`**

There is no existing host test file that exercises `memory locate`/`memory forget`/`memory record` (they were added without unit tests), so this task introduces the first one for the memory-verb family, over a fake `contracts.Memory` registered as the sole memory plugin for this test binary.

```go
package host

import (
	"context"
	"fmt"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	orchestrator "github.com/Herrscherd/herrscher-orchestrator"
)

// restoreVerbMem is a minimal fake contracts.Memory for exercising `memory
// restore` end to end through the real registry dispatch path (no obsidian
// vault involved). Recall errors on an absent key, matching the real contract
// orchestrator.Restore relies on.
type restoreVerbMem struct {
	nodes map[string]contracts.Node
}

func (m *restoreVerbMem) Recall(_ context.Context, key string, _ int) (contracts.Subgraph, error) {
	n, ok := m.nodes[key]
	if !ok {
		return contracts.Subgraph{}, fmt.Errorf("restoreVerbMem: no node at key %q", key)
	}
	return contracts.Subgraph{Root: n}, nil
}
func (m *restoreVerbMem) Record(_ context.Context, n contracts.Node) error {
	m.nodes[n.Key] = n
	return nil
}
func (m *restoreVerbMem) Search(context.Context, contracts.Query) ([]contracts.Node, error) {
	return nil, nil
}
func (m *restoreVerbMem) Links(context.Context, string, string, string) error { return nil }
func (m *restoreVerbMem) Close() error                                       { return nil }

// currentRestoreMem is swapped by each test before building a registry.
// Registered exactly once below (init) as the package's only fake memory
// plugin, so BuildFirstMemory's "first registered memory plugin" contract
// deterministically resolves to whatever the current test set.
var currentRestoreMem *restoreVerbMem

func init() {
	contracts.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "fake-restore-verb", Category: contracts.CategoryMemory},
		Memory: func(context.Context, contracts.PluginConfig) (contracts.Memory, error) {
			return currentRestoreMem, nil
		},
	})
}

func TestMemoryRestoreVerbReactivatesArchivedNode(t *testing.T) {
	currentRestoreMem = &restoreVerbMem{nodes: map[string]contracts.Node{
		"facts/a": {Key: "facts/a", Meta: map[string]string{
			contracts.MetaState:    contracts.StateArchived,
			contracts.MetaLastSeen: "2020-01-01T00:00:00Z",
		}},
	}}
	reg, err := NewRegistry(context.Background(), Deps{}, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Dispatch(context.Background(), []string{"memory", "restore", "--key", "facts/a"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if out != "restored facts/a" {
		t.Errorf("output = %q, want %q", out, "restored facts/a")
	}
	got := currentRestoreMem.nodes["facts/a"]
	if got.Meta[contracts.MetaState] != contracts.StateActive {
		t.Errorf("state = %q, want active", got.Meta[contracts.MetaState])
	}
}

func TestMemoryRestoreVerbForceDetachesMergedOriginal(t *testing.T) {
	currentRestoreMem = &restoreVerbMem{nodes: map[string]contracts.Node{
		"facts/a": {Key: "facts/a", Meta: map[string]string{
			orchestrator.MetaMergedInto: "facts/u",
			contracts.MetaState:         contracts.StateArchived,
			contracts.MetaLastSeen:      "2020-01-01T00:00:00Z",
		}},
	}}
	reg, err := NewRegistry(context.Background(), Deps{}, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Dispatch(context.Background(), []string{"memory", "restore", "--key", "facts/a"})
	if err == nil || !strings.Contains(err.Error(), "restore with Force") {
		t.Fatalf("without --force expected ErrMergedOriginal, got %v", err)
	}
	out, err := reg.Dispatch(context.Background(), []string{"memory", "restore", "--key", "facts/a", "--force", "true"})
	if err != nil {
		t.Fatalf("dispatch with force: %v", err)
	}
	if out != "restored facts/a" {
		t.Errorf("output = %q, want %q", out, "restored facts/a")
	}
	got := currentRestoreMem.nodes["facts/a"]
	if got.Meta[contracts.MetaState] != contracts.StateActive || got.Meta[orchestrator.MetaMergedInto] != "" {
		t.Fatalf("force-restore did not detach: %+v", got.Meta)
	}
}

func TestMemoryRestoreVerbUnknownKeyErrors(t *testing.T) {
	currentRestoreMem = &restoreVerbMem{nodes: map[string]contracts.Node{}}
	reg, err := NewRegistry(context.Background(), Deps{}, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Dispatch(context.Background(), []string{"memory", "restore", "--key", "nope"}); err == nil {
		t.Fatal("expected an error restoring an unknown key")
	}
}
```

- [ ] **Step 2: Run to verify FAIL**

Run: `cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review && go test ./core/host/... -run TestMemoryRestoreVerb -v`
Expected: FAIL — `unknown command "memory restore"` (the command is not registered yet).

- [ ] **Step 3: Add the non-blank `orchestrator` import to `cli.go`**

In the import block of `core/host/cli.go`, add (alphabetically among the `github.com/Herrscherd/...` imports, matching the existing precedent in `b3_consolidation_test.go`/`bridge_test.go`):

```go
	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/forge"
	"github.com/Herrscherd/herrscher/core/internal/manager"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
	"github.com/Herrscherd/herrscher/core/service"
	orchestrator "github.com/Herrscherd/herrscher-orchestrator"
```

(gofmt/goimports will re-sort this block by import path; run it in Step 5 rather than hand-ordering.)

- [ ] **Step 4: Add the `memory restore` command in `buildRegistry`**

In `core/host/cli.go`, after the `memory record` block and before `return reg, hostDeps{wt: wt, agents: agents, handler: hdl, seedCoord: seedCoord}, nil`, add:

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

- [ ] **Step 5: gofmt the import block, run to verify PASS + full package suite**

```bash
cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review
gofmt -w core/host/cli.go core/host/memory_restore_verb_test.go
```

Run: `go test ./core/host/... -run TestMemoryRestoreVerb -v` → all 3 PASS.
Run: `go test ./core/host/...` → PASS (existing host tests unaffected).
Run: `go vet ./core/host/...` → clean.

- [ ] **Step 6: Commit**

```bash
cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review
git add core/host/cli.go core/host/memory_restore_verb_test.go
git commit -m "feat(host): add memory restore CLI verb over orchestrator.Restore"
```

---

## Post-implementation (main agent, after all tasks reviewed clean)

1. **Final whole-branch review** (most capable model) over both diffs — orchestrator (Tasks 1-5) and host (Task 6).
2. **Release wiring:** tag `herrscher-orchestrator` **v0.1.13** at the reviewed HEAD (`git tag v0.1.13 && git push origin v0.1.13` in `/home/shan/dev/herrscher-orchestrator`, a separate repo used at master). In the host: remove the dev `go.work`/`go.work.sum` overlay (`rm go.work go.work.sum`, or just leave them untracked and out of the commit — they must never be committed), bump `go.mod` orchestrator v0.1.12 → v0.1.13, `GOWORK=off go mod tidy`, `GOWORK=off go build ./... && GOWORK=off go test ./...` GREEN.
3. **Docs:** README "Learning (the write side)" gains a **Reversible archive + audit report** paragraph; update the `herrscher-memory-vs-hermes` memory note: G4 moves from "herrscher deletes" (stale — G3 already made it label-based) to "shipped: report + restore, no delete, no contracts/obsidian change."
4. **finishing-a-development-branch:** merge `origin/master` into the host branch first (shared-worktree rule), PR → master, GitHub-side merge.

---

## Self-Review

**Spec coverage:**
- §1 `Transition` type + `Curator.transitions` + Sweep append-after-success → Task 1 ✓
- §1 Merge appends umbrella + per-original transitions, best-effort on a failed original → Task 2 ✓
- §2 `Restore` free function, `Force`, `ErrMergedOriginal`, absent-key-is-an-error, idempotent-on-active, force-detach → Task 3 ✓
- §2 `Learner.Restore` wrapper appending a `Transition{Kind:"restore"}` → Task 3 ✓
- §3 `ReportKind`, `reportPrefix` default, report body (table + per-Kind counts), skip-when-disabled-or-empty, new key per run, best-effort fold into `firstErr`, `l.transitions = nil` reset unconditional → Task 4 ✓
- §4 host `memory restore` verb mirroring `memory forget`, non-blank `orchestrator` import → Task 6 ✓
- §5 config triple (`report-enabled`/`MEMORY_REPORT_ENABLED`, `report-prefix`/`MEMORY_REPORT_PREFIX`) + `SetReport` wiring in `register.go` → Task 5 ✓
- Idempotence section (pass-scoped transitions, freshly-keyed reports, `Restore` idempotent-on-active / well-defined-error-on-merged / safe-to-force-twice) → covered by Task 1/2's reset semantics + Task 3's tests (`TestRestoreHappyPath`, `TestRestoreWithForceDetaches`) ✓
- Testing list in the spec (11 bullets: sweep transitions, sweep write-failure, merge two-transitions, report-skipped, report-shape, report-disabled, restore happy/refuse/force/absent, `Learner.Restore`, host CLI) → every bullet has a named test in Tasks 1-6 ✓
- Release footprint (v0.1.13, host bump, README/memory-note update) → Post-implementation section ✓
- Out-of-scope items (archive/ dir, report rotation, single-rewritten-report-file, `memory report` verb, restoring a merely-stale node) → intentionally NOT implemented anywhere in this plan; no task touches them ✓
- Three invariants → stated verbatim in Global Constraints; Task 1/2/4 comments cite invariant 2 inline, Task 3 cites invariant 3, restore.go's doc comment cites invariant 1's file-layout-ignorance rationale ✓

**Placeholder scan:** no "TBD"/"handle appropriately"/"similar to Task N" phrasing found; every step carries complete, runnable Go code or an exact shell command with its expected output.

**Type consistency:** `Transition{Key, From, To, Kind}` fields used identically in Tasks 1, 2, 3, 4. `Restore(ctx, mem, key, opts...)` / `Force(bool) RestoreOption` / `ErrMergedOriginal` signatures identical between Task 3's definition and Task 6's host usage. `SetReport(enabled bool, prefix string)` identical between Task 4's definition and Task 5's call site. `ReportKind`/`defaultReportPrefix` defined once (Task 4) and consumed by name in Task 4's own tests only (Task 5 needs no Go symbol from `report.go` beyond the setter). Fakes: `mergeMem`/`plainExt`/`stale`/`learnerWith` defined once in `merge_test.go` (pre-existing) and reused by Tasks 2 and 4; `restoreMem` is a new, deliberately different fake introduced in Task 3 (documented why: existing fakes don't model an absent-key Recall error, which Restore's contract requires); `restoreVerbMem` in Task 6 is host-side and independent (different package, different port surface — no obsidian/orchestrator fake naming collision since packages differ).
