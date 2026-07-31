# G2 Semantic Merge (umbrellas) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a best-effort semantic-merge pass to the orchestrator's Learner that folds semantically overlapping memory nodes into a single umbrella node, reversibly, via a type-asserted `Merger` seam the closed moat implements.

**Architecture:** New `Merger` seam + `Umbrella` type + `Learner.Merge(ctx)` loop live in `herrscher-orchestrator` only (new file `merge.go`). Merge groups eligible nodes by `Meta["domain"]`, calls the wired merger per group, and applies each validated umbrella by writing the fused node then labeling/archiving/linking each original with an orchestrator-local `mergedInto` marker. `Curator.Sweep` gains a one-line guard so merged originals are never reactivated. Config exposed as three flag+env+settings triples wired via a new `SetMerge` setter. No `herrscher-contracts` / `herrscher-obsidian-memory` change.

**Tech Stack:** Go, module `github.com/Herrscherd/herrscher-orchestrator`, dep `github.com/Herrscherd/herrscher-contracts` (already-released v0.2.9). Tests use in-package fakes (`go test ./...`).

## Global Constraints

- **Scope:** `herrscher-orchestrator` ONLY. No contracts / obsidian change. Release → **v0.1.11**; host `go.mod` bumps orchestrator only.
- **Three umbrella invariants (must all hold):** (1) Ports only, policy not engine — seam + plumbing over the existing `contracts.Memory` port, no new port method, no storage engine. (2) Learning never breaks a turn — `Merge` is best-effort, called as `_ = l.Merge(ctx)` from `Consolidate`, whose result is swallowed by `Observe`. (3) Reversible over destructive — originals are labeled + archived + linked, never deleted or overwritten with different content; the umbrella is a NEW node.
- **Opt-in default off:** `mergeMin <= 0` → `Merge` is a clean no-op. A Learner with no `SetMerge` call (or a plain `Curator`) does nothing new.
- **No-merger no-op:** if `l.extract` does not implement `Merger`, `Merge` returns nil without touching memory.
- **Marker constant:** `const MetaMergedInto = "mergedInto"` lives in `orchestrator` (obsidian stores Meta generically). Same package → `Sweep` reads `n.Meta[MetaMergedInto]` directly.
- **Contracts symbols to use verbatim:** `contracts.Query{}`, `contracts.Node{Key,Body,Meta}`, `mem.Record(ctx, n)`, `mem.Links(ctx, from, to, rel string)`, `mem.Search(ctx, contracts.Query)`, `contracts.MetaState` (`"state"`), `contracts.StateActive/StateStale/StateArchived` (`"active"/"stale"/"archived"`), `contracts.MetaLastSeen` (`"lastSeen"`).
- **Best-effort accumulation pattern:** mirror `Sweep`/`Consolidate` — record the first error, keep going; one bad group/original must not abort the rest.
- Run `gofmt -l` and `go vet ./...` clean before each commit.

---

## File Structure

- **Create `merge.go`** — `MetaMergedInto` const, `Umbrella` struct, `Merger` interface, `Learner.merger()`, `Learner.Merge(ctx)`, `Learner.mergeEligible(n)`, `Learner.applyUmbrella(...)`, `Learner.validUmbrella(...)`, `defaultMergeMax` const, `SetMerge` setter. (Merge loop is a Learner method — the merger is discovered off `l.extract`, same as G1's `Consolidator`.)
- **Modify `learner.go`** — add `mergeMin/mergeMax/mergeTarget` fields to `Learner`; wire `_ = l.Merge(ctx)` into `Consolidate` after the sweep.
- **Modify `sweep.go`** — one-line guard: skip nodes with `Meta[MetaMergedInto] != ""`.
- **Modify `register.go`** — three new `Setting`s; read + `l.SetMerge(...)` in the Learner branch.
- **Create `merge_test.go`** — fakes (`mergeMem`, `fakeMerger`, `plainExt`) + all merge/apply/guard/config tests.

---

### Task 1: Merger seam + Merge loop + reversible apply

**Files:**
- Create: `merge.go`
- Modify: `learner.go` (struct fields + `Consolidate` wiring)
- Test: `merge_test.go`

**Interfaces:**
- Consumes: `Learner{*Curator, extract Extractor, mem (via Curator)}`; `contracts.Memory` (`Record`, `Search`, `Links`); `contracts.Node`, `contracts.Query{}`; `contracts.MetaState`, `contracts.StateActive/StateStale/StateArchived`.
- Produces: `type Umbrella struct{ Node contracts.Node; Merged []string }`; `type Merger interface{ Merge(ctx, []contracts.Node) ([]Umbrella, error) }`; `const MetaMergedInto = "mergedInto"`; `func (l *Learner) Merge(ctx) error`; `func (l *Learner) SetMerge(minNodes, max int, target string)`. Task 2 consumes `MetaMergedInto`; Task 3 consumes `SetMerge`.

- [ ] **Step 1: Write `merge.go`**

```go
package orchestrator

import (
	"context"
	"log/slog"

	"github.com/Herrscherd/herrscher-contracts"
)

// defaultMergeMax caps the number of nodes handed to a Merger per domain group,
// bounding the closed LLM pass's prompt size / cost when a caller passes max<=0.
const defaultMergeMax = 40

// MetaMergedInto, when set on a node, names the umbrella Key that subsumed it.
// It is a terminal marker: the node is kept on disk (reversible) but archived
// and excluded from recall. Orchestrator-internal — obsidian stores Meta
// generically, so no contracts change is needed.
const MetaMergedInto = "mergedInto"

// Umbrella is one merge proposal from a Merger: a fused node (Node) that
// subsumes the originals named by Merged (their Keys, >= 2). The plumbing writes
// Node, then labels, links, and archives each original. The fused node's
// Key/Title/Body/Meta and the overlap decision are the closed merger's to make;
// this package only validates and applies.
type Umbrella struct {
	Node   contracts.Node
	Merged []string
}

// Merger fuses semantically overlapping candidates into umbrella nodes (memory
// roadmap G2). Given a pre-filtered, single-domain slice of candidates it returns
// zero or more umbrellas; an empty result means "nothing worth merging". The
// heuristics are the closed part of the moat; this package defines only the seam
// and the open plumbing (Learner.Merge) that drives it.
type Merger interface {
	Merge(ctx context.Context, cands []contracts.Node) ([]Umbrella, error)
}

// SetMerge configures the G2 semantic-merge pass. minNodes <= 0 disables it;
// max <= 0 falls back to defaultMergeMax; an unrecognised target falls back to
// "stale".
func (l *Learner) SetMerge(minNodes, max int, target string) {
	l.mergeMin = minNodes
	if max <= 0 {
		max = defaultMergeMax
	}
	l.mergeMax = max
	switch target {
	case "all", "active", "stale":
		l.mergeTarget = target
	default:
		l.mergeTarget = "stale"
	}
}

// merger returns the Merger the extractor also implements, if any. The closed
// extractor typically implements Extract, Consolidate, and Merge, so the merge
// pass needs no new constructor parameter.
func (l *Learner) merger() (Merger, bool) {
	m, ok := l.extract.(Merger)
	return m, ok
}

// Merge groups eligible non-archived nodes by Meta["domain"] and folds each
// group of at least mergeMin nodes into an umbrella via the wired Merger. It is
// best-effort: disabled (mergeMin<=0), no merger, or nil Memory all yield a clean
// no-op, and a per-group/per-node error is recorded but never aborts the rest.
func (l *Learner) Merge(ctx context.Context) error {
	if l.mergeMin <= 0 || l.mem == nil {
		return nil
	}
	m, ok := l.merger()
	if !ok {
		return nil
	}
	nodes, err := l.mem.Search(ctx, contracts.Query{}) // active+stale, never archived
	if err != nil {
		return err
	}
	groups := map[string][]contracts.Node{}
	for _, n := range nodes {
		if n.Meta[MetaMergedInto] != "" {
			continue // already folded; terminal
		}
		if !l.mergeEligible(n) {
			continue
		}
		groups[n.Meta["domain"]] = append(groups[n.Meta["domain"]], n)
	}
	var firstErr error
	for _, group := range groups {
		if len(group) < l.mergeMin {
			continue
		}
		if len(group) > l.mergeMax {
			group = group[:l.mergeMax]
		}
		umbrellas, merr := m.Merge(ctx, group)
		if merr != nil {
			if firstErr == nil {
				firstErr = merr // record and keep going: one bad group must not
			} // abort the others
			continue
		}
		for _, u := range umbrellas {
			if aerr := l.applyUmbrella(ctx, u, group); aerr != nil && firstErr == nil {
				firstErr = aerr
			}
		}
	}
	return firstErr
}

// mergeEligible reports whether node n is in scope for the current merge target.
// "all" = anything not archived; "active" = active/absent-state only; "stale"
// (default) = stale only.
func (l *Learner) mergeEligible(n contracts.Node) bool {
	state := n.Meta[contracts.MetaState]
	if state == "" {
		state = contracts.StateActive
	}
	switch l.mergeTarget {
	case "all":
		return state != contracts.StateArchived
	case "active":
		return state == contracts.StateActive
	default: // "stale"
		return state == contracts.StateStale
	}
}

// applyUmbrella validates one Umbrella against the group it came from, then — if
// valid — writes the fused node and labels/archives/links each original. A
// malformed proposal is rejected (WARN, skipped) so it cannot corrupt the graph
// or drop valid proposals from the same batch. Per-original write failures are
// best-effort (record first, continue) so one bad original never strands the
// umbrella or its siblings.
func (l *Learner) applyUmbrella(ctx context.Context, u Umbrella, group []contracts.Node) error {
	byKey := make(map[string]contracts.Node, len(group))
	for _, n := range group {
		byKey[n.Key] = n
	}
	if !l.validUmbrella(u, byKey) {
		return nil // rejected (WARN already emitted)
	}
	if err := l.mem.Record(ctx, u.Node); err != nil {
		return err
	}
	var firstErr error
	for _, k := range u.Merged {
		orig := byKey[k]
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
			continue
		}
		if err := l.mem.Links(ctx, k, u.Node.Key, "merged-into"); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// validUmbrella rejects (WARN + false) an umbrella that would corrupt the graph:
// empty Key/Body, fewer than 2 merged originals, a Key that collides with a
// candidate node (an umbrella must be a NEW node, never an original reused), or a
// merged Key outside the candidate group.
func (l *Learner) validUmbrella(u Umbrella, byKey map[string]contracts.Node) bool {
	reason := ""
	switch {
	case u.Node.Key == "":
		reason = "empty umbrella key"
	case u.Node.Body == "":
		reason = "empty umbrella body"
	case len(u.Merged) < 2:
		reason = "fewer than 2 originals"
	}
	if reason == "" {
		if _, collides := byKey[u.Node.Key]; collides {
			reason = "umbrella key reuses a candidate node"
		}
	}
	if reason == "" {
		for _, k := range u.Merged {
			if _, ok := byKey[k]; !ok {
				reason = "merged key outside candidate group"
				break
			}
		}
	}
	if reason != "" {
		slog.Warn("memory: rejecting invalid umbrella",
			"key", u.Node.Key, "reason", reason, "merged", len(u.Merged))
		return false
	}
	return true
}
```

- [ ] **Step 2: Add the three fields to the `Learner` struct in `learner.go`**

Append inside the `Learner` struct (after the `pending []Candidate` field, before the closing `}`):

```go
	// mergeMin/mergeMax/mergeTarget configure the G2 semantic-merge pass
	// (Learner.Merge). mergeMin <= 0 disables it (opt-in, default off); set via
	// SetMerge. mergeTarget is one of "stale" (default) / "active" / "all".
	mergeMin    int
	mergeMax    int
	mergeTarget string
```

- [ ] **Step 3: Wire `Merge` into `Consolidate` in `learner.go`**

In `Consolidate`, change the tail from:

```go
	_ = l.Sweep(ctx)
	return firstErr
```

to:

```go
	_ = l.Sweep(ctx)
	// Best-effort semantic merge after the sweep (opt-in via SetMerge; a no-op
	// when disabled or no Merger is wired). A merge error must never propagate
	// out of Consolidate (invariant: learning never breaks a turn).
	_ = l.Merge(ctx)
	return firstErr
```

- [ ] **Step 4: Write `merge_test.go` — fakes + tests**

```go
package orchestrator

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

// mergeMem is a fake Memory: Search returns a fixed node set, Record/Links
// capture writes. recErrOn, if set, makes Record fail for that one Key.
type mergeMem struct {
	nodes    []contracts.Node
	records  []contracts.Node
	links    [][3]string
	recErrOn string
}

func (m *mergeMem) Record(_ context.Context, n contracts.Node) error {
	if m.recErrOn != "" && n.Key == m.recErrOn {
		return context.DeadlineExceeded // any non-budget error
	}
	m.records = append(m.records, n)
	return nil
}
func (m *mergeMem) Recall(_ context.Context, key string, _ int) (contracts.Subgraph, error) {
	return contracts.Subgraph{Root: contracts.Node{Key: key}}, nil
}
func (m *mergeMem) Search(context.Context, contracts.Query) ([]contracts.Node, error) {
	return m.nodes, nil
}
func (m *mergeMem) Links(_ context.Context, from, to, rel string) error {
	m.links = append(m.links, [3]string{from, to, rel})
	return nil
}
func (m *mergeMem) Close() error { return nil }

// fakeMerger is an Extractor that ALSO implements Merger. It records each Merge
// call's candidate slice and returns a fixed result.
type fakeMerger struct {
	calls  [][]contracts.Node
	result []Umbrella
	err    error
}

func (f *fakeMerger) Extract(context.Context, string, string) ([]Candidate, error) {
	return nil, nil
}
func (f *fakeMerger) Merge(_ context.Context, cands []contracts.Node) ([]Umbrella, error) {
	f.calls = append(f.calls, cands)
	return f.result, f.err
}

// plainExt is an Extractor that does NOT implement Merger.
type plainExt struct{}

func (plainExt) Extract(context.Context, string, string) ([]Candidate, error) { return nil, nil }

// stale builds a stale node with a domain.
func stale(key, domain string) contracts.Node {
	return contracts.Node{Key: key, Body: key, Meta: map[string]string{
		"domain": domain, contracts.MetaState: contracts.StateStale,
	}}
}

// learnerWith builds a Learner over mem+ex with merge configured.
func learnerWith(mem contracts.Memory, ex Extractor, min, max int, target string) *Learner {
	l := NewLearner(mem, "s", contracts.MemoryScope{}, ex, "", 0)
	l.SetMerge(min, max, target)
	return l
}

func TestMergeNoMergerIsNoop(t *testing.T) {
	mem := &mergeMem{nodes: []contracts.Node{stale("a", "d"), stale("b", "d")}}
	l := learnerWith(mem, plainExt{}, 2, 40, "stale")
	if err := l.Merge(context.Background()); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(mem.records) != 0 || len(mem.links) != 0 {
		t.Fatalf("no-merger must be a no-op; got %d records %d links", len(mem.records), len(mem.links))
	}
}

func TestMergeDisabledWhenMinZero(t *testing.T) {
	mem := &mergeMem{nodes: []contracts.Node{stale("a", "d"), stale("b", "d")}}
	f := &fakeMerger{}
	l := learnerWith(mem, f, 0, 40, "stale") // min 0 => off
	if err := l.Merge(context.Background()); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("mergeMin<=0 must not call the merger; got %d calls", len(f.calls))
	}
}

func TestMergeBelowThresholdNoCall(t *testing.T) {
	mem := &mergeMem{nodes: []contracts.Node{stale("a", "d")}} // 1 < min 2
	f := &fakeMerger{}
	l := learnerWith(mem, f, 2, 40, "stale")
	if err := l.Merge(context.Background()); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("below threshold must not call merger; got %d", len(f.calls))
	}
}

func TestMergeHappyPathWritesAndArchives(t *testing.T) {
	a, b := stale("facts/a", "d"), stale("facts/b", "d")
	a.Meta[contracts.MetaLastSeen] = "2026-01-01T00:00:00Z"
	mem := &mergeMem{nodes: []contracts.Node{a, b}}
	f := &fakeMerger{result: []Umbrella{{
		Node:   contracts.Node{Key: "facts/umbrella", Body: "fused"},
		Merged: []string{"facts/a", "facts/b"},
	}}}
	l := learnerWith(mem, f, 2, 40, "stale")
	if err := l.Merge(context.Background()); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	// umbrella written
	var sawUmbrella bool
	for _, r := range mem.records {
		if r.Key == "facts/umbrella" && r.Body == "fused" {
			sawUmbrella = true
		}
	}
	if !sawUmbrella {
		t.Fatal("umbrella node was not recorded")
	}
	// each original archived + labeled + lastSeen preserved
	for _, k := range []string{"facts/a", "facts/b"} {
		var got *contracts.Node
		for i := range mem.records {
			if mem.records[i].Key == k {
				got = &mem.records[i]
			}
		}
		if got == nil {
			t.Fatalf("original %s not re-recorded", k)
		}
		if got.Meta[MetaMergedInto] != "facts/umbrella" {
			t.Errorf("%s: mergedInto=%q, want facts/umbrella", k, got.Meta[MetaMergedInto])
		}
		if got.Meta[contracts.MetaState] != contracts.StateArchived {
			t.Errorf("%s: state=%q, want archived", k, got.Meta[contracts.MetaState])
		}
	}
	// lastSeen of facts/a preserved (not bumped)
	for i := range mem.records {
		if mem.records[i].Key == "facts/a" && mem.records[i].Meta[contracts.MetaLastSeen] != "2026-01-01T00:00:00Z" {
			t.Errorf("facts/a lastSeen changed to %q", mem.records[i].Meta[contracts.MetaLastSeen])
		}
	}
	// links original -> umbrella
	want := map[string]bool{"facts/a": false, "facts/b": false}
	for _, ln := range mem.links {
		if ln[1] == "facts/umbrella" && ln[2] == "merged-into" {
			want[ln[0]] = true
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("missing merged-into link from %s", k)
		}
	}
}

func TestMergeRejectsInvalidUmbrellaKeepsValid(t *testing.T) {
	mem := &mergeMem{nodes: []contracts.Node{stale("facts/a", "d"), stale("facts/b", "d")}}
	f := &fakeMerger{result: []Umbrella{
		{Node: contracts.Node{Key: "facts/bad", Body: "x"}, Merged: []string{"facts/a"}},          // <2
		{Node: contracts.Node{Key: "", Body: "x"}, Merged: []string{"facts/a", "facts/b"}},        // empty key
		{Node: contracts.Node{Key: "facts/e", Body: ""}, Merged: []string{"facts/a", "facts/b"}},  // empty body
		{Node: contracts.Node{Key: "facts/a", Body: "x"}, Merged: []string{"facts/a", "facts/b"}}, // key reuses candidate
		{Node: contracts.Node{Key: "facts/f", Body: "x"}, Merged: []string{"facts/a", "facts/z"}}, // key outside group
		{Node: contracts.Node{Key: "facts/good", Body: "ok"}, Merged: []string{"facts/a", "facts/b"}},
	}}
	l := learnerWith(mem, f, 2, 40, "stale")
	if err := l.Merge(context.Background()); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	umbrellas := 0
	for _, r := range mem.records {
		if strings.HasPrefix(r.Key, "facts/") && r.Body == "ok" {
			umbrellas++
		}
		if r.Key == "facts/bad" || r.Key == "facts/e" || r.Key == "facts/f" {
			t.Errorf("invalid umbrella %s was recorded", r.Key)
		}
	}
	if umbrellas != 1 {
		t.Fatalf("expected exactly the 1 valid umbrella recorded, got %d", umbrellas)
	}
}

func TestMergeGroupsByDomainNotMixed(t *testing.T) {
	// two domains, each below threshold (1), jointly 2 — must NOT merge together.
	mem := &mergeMem{nodes: []contracts.Node{stale("a", "d1"), stale("b", "d2")}}
	f := &fakeMerger{}
	l := learnerWith(mem, f, 2, 40, "stale")
	if err := l.Merge(context.Background()); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("distinct domains below threshold must not be merged; got %d calls", len(f.calls))
	}
}

func TestMergeRespectsCap(t *testing.T) {
	var nodes []contracts.Node
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		nodes = append(nodes, stale(k, "d"))
	}
	mem := &mergeMem{nodes: nodes}
	f := &fakeMerger{}
	l := learnerWith(mem, f, 2, 3, "stale") // cap 3
	if err := l.Merge(context.Background()); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(f.calls) != 1 || len(f.calls[0]) != 3 {
		t.Fatalf("cap not respected: want 1 call of 3 nodes, got %d calls, first len %d", len(f.calls), func() int {
			if len(f.calls) == 0 {
				return -1
			}
			return len(f.calls[0])
		}())
	}
}

func TestMergeTargetFiltersToStale(t *testing.T) {
	active := stale("act", "d")
	active.Meta[contracts.MetaState] = contracts.StateActive
	mem := &mergeMem{nodes: []contracts.Node{stale("s1", "d"), stale("s2", "d"), active}}
	f := &fakeMerger{}
	l := learnerWith(mem, f, 2, 40, "stale")
	if err := l.Merge(context.Background()); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(f.calls))
	}
	for _, n := range f.calls[0] {
		if n.Key == "act" {
			t.Error("active node leaked into a stale-target merge")
		}
	}
}

func TestMergeBestEffortOnOriginalRecordError(t *testing.T) {
	mem := &mergeMem{
		nodes:    []contracts.Node{stale("facts/a", "d"), stale("facts/b", "d")},
		recErrOn: "facts/a", // archiving facts/a fails
	}
	f := &fakeMerger{result: []Umbrella{{
		Node:   contracts.Node{Key: "facts/u", Body: "fused"},
		Merged: []string{"facts/a", "facts/b"},
	}}}
	l := learnerWith(mem, f, 2, 40, "stale")
	// error is surfaced (non-nil) but the umbrella + sibling still applied.
	_ = l.Merge(context.Background())
	var sawUmbrella, sawB bool
	for _, r := range mem.records {
		if r.Key == "facts/u" {
			sawUmbrella = true
		}
		if r.Key == "facts/b" && r.Meta[contracts.MetaState] == contracts.StateArchived {
			sawB = true
		}
	}
	if !sawUmbrella {
		t.Error("umbrella not written despite a per-original failure")
	}
	if !sawB {
		t.Error("sibling facts/b not archived despite facts/a failing")
	}
}
```

- [ ] **Step 5: Run the tests, expect FAIL then PASS**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run Merge -v`
Expected first: compile error / FAIL until `merge.go` + fields + wiring exist; after Steps 1-3, all `TestMerge*` PASS. Then run the full suite: `go test ./...` → PASS.

- [ ] **Step 6: gofmt + vet + commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
gofmt -w merge.go merge_test.go learner.go
go vet ./...
git add merge.go merge_test.go learner.go
git commit -m "feat(learner): G2 semantic-merge pass — Merger seam + reversible umbrellas"
```

---

### Task 2: Sweep guard — never reactivate a merged original

**Files:**
- Modify: `sweep.go:30` (the per-node loop)
- Test: `merge_test.go` (append)

**Interfaces:**
- Consumes: `MetaMergedInto` (Task 1); `Curator.Sweep`, `contracts.NextState`, `contracts.MetaState`, `contracts.StateArchived`, `contracts.MetaLastSeen`.
- Produces: nothing new.

- [ ] **Step 1: Write the failing test (append to `merge_test.go`)**

```go
func TestSweepSkipsMergedOriginals(t *testing.T) {
	// A merged original with a FRESH lastSeen would, without the guard, be
	// re-derived as active and reactivated. The guard must keep it archived.
	fresh := "2026-07-30T00:00:00Z"
	n := contracts.Node{Key: "facts/a", Body: "x", Meta: map[string]string{
		MetaMergedInto:          "facts/u",
		contracts.MetaState:     contracts.StateArchived,
		contracts.MetaLastSeen:  fresh,
	}}
	mem := &mergeMem{nodes: []contracts.Node{n}}
	c := NewScoped(mem, "s", contracts.MemoryScope{})
	c.SetStaleness(30*24*hour, 90*24*hour)
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	for _, r := range mem.records {
		if r.Key == "facts/a" {
			t.Fatalf("merged original was rewritten by Sweep (state now %q)", r.Meta[contracts.MetaState])
		}
	}
}
```

Add a `hour` helper near the top of `merge_test.go` if not already present:

```go
const hour = 60 * 60 * 1e9 // time.Hour as untyped; use time.Duration in call
```

If that untyped form is awkward, instead import `"time"` in `merge_test.go` and call `c.SetStaleness(30*24*time.Hour, 90*24*time.Hour)` directly — pick whichever keeps the file gofmt-clean and drop the `hour` const.

- [ ] **Step 2: Run to verify it FAILS**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestSweepSkipsMergedOriginals -v`
Expected: FAIL — `Sweep` rewrites `facts/a` because a fresh lastSeen re-derives to active.

- [ ] **Step 3: Add the guard in `sweep.go`**

Inside `func (c *Curator) Sweep`, at the very top of the `for _, n := range nodes {` loop body (before the `stamp := ...` line), insert:

```go
		if n.Meta[MetaMergedInto] != "" {
			continue // merged originals are terminal; never reactivate a folded fragment
		}
```

- [ ] **Step 4: Run to verify it PASSES + full suite**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestSweepSkipsMergedOriginals -v` → PASS
Run: `go test ./...` → PASS

- [ ] **Step 5: gofmt + vet + commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
gofmt -w sweep.go merge_test.go
go vet ./...
git add sweep.go merge_test.go
git commit -m "fix(sweep): skip mergedInto originals so a fold is never reactivated"
```

---

### Task 3: Config surface — flag/env/settings triple + factory wiring

**Files:**
- Modify: `register.go`
- Test: `merge_test.go` (append — exercises `SetMerge` defaults; the manifest wiring is covered by build + existing plugin-registration test if any)

**Interfaces:**
- Consumes: `SetMerge` (Task 1); `contracts.Setting`, `contracts.PluginConfig.Get`, `strconv.Atoi` (already imported in `register.go`).
- Produces: three declared settings (`merge-min-nodes` / `merge-target` / `merge-max`).

- [ ] **Step 1: Add the three `Setting`s to the manifest in `register.go`**

In the `Config: []contracts.Setting{ ... }` block, after the `archive-days` line, add:

```go
				{Key: "merge-min-nodes", Env: "MEMORY_MERGE_MIN", Help: "min nodes in a domain group before the merge pass folds them into an umbrella; <=0 disables (default 0, off)", Required: false},
				{Key: "merge-target", Env: "MEMORY_MERGE_TARGET", Help: "which nodes the merge pass considers: stale | active | all (default stale)", Required: false},
				{Key: "merge-max", Env: "MEMORY_MERGE_MAX", Help: "cap on nodes handed to the merger per domain group (default 40)", Required: false},
```

- [ ] **Step 2: Wire `SetMerge` in the Learner branch of the factory**

In the `if ex := lookupExtractor(...); ex != nil {` block, after `l.SetStaleness(stale, archive)` and before `return l, nil`, add:

```go
				mergeMin, _ := strconv.Atoi(cfg.Get("merge-min-nodes"))
				mergeMax, _ := strconv.Atoi(cfg.Get("merge-max"))
				l.SetMerge(mergeMin, mergeMax, cfg.Get("merge-target"))
```

(No change to the plain-`Curator` branch: without an extractor there is no merger, so the merge pass cannot run regardless.)

- [ ] **Step 3: Write the `SetMerge` defaults test (append to `merge_test.go`)**

```go
func TestSetMergeDefaults(t *testing.T) {
	l := NewLearner(&mergeMem{}, "s", contracts.MemoryScope{}, &fakeMerger{}, "", 0)
	l.SetMerge(3, 0, "bogus") // max<=0 -> default; bad target -> stale
	if l.mergeMax != defaultMergeMax {
		t.Errorf("mergeMax=%d, want %d", l.mergeMax, defaultMergeMax)
	}
	if l.mergeTarget != "stale" {
		t.Errorf("mergeTarget=%q, want stale", l.mergeTarget)
	}
	l.SetMerge(2, 10, "active")
	if l.mergeMax != 10 || l.mergeTarget != "active" || l.mergeMin != 2 {
		t.Errorf("SetMerge did not apply valid values: min=%d max=%d target=%q", l.mergeMin, l.mergeMax, l.mergeTarget)
	}
}
```

- [ ] **Step 4: Run tests + full suite**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestSetMergeDefaults -v` → PASS
Run: `go test ./...` → PASS (register.go still compiles + registers)

- [ ] **Step 5: gofmt + vet + commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
gofmt -w register.go merge_test.go
go vet ./...
git add register.go merge_test.go
git commit -m "feat(config): expose merge-min-nodes/merge-target/merge-max settings"
```

---

## Post-implementation (main agent, after all tasks reviewed clean)

1. **Final whole-branch review** (most capable model) over the orchestrator diff.
2. **Release wiring:** tag `herrscher-orchestrator` **v0.1.11** at the reviewed HEAD; in the host, bump `go.mod` orchestrator v0.1.10 → v0.1.11 (contracts/obsidian untouched), `GOWORK=off go mod tidy`, `GOWORK=off go build ./... && go test ./...` GREEN.
3. **Docs:** README "Learning (the write side)" gains a **Semantic merge** paragraph; update `herrscher-memory-vs-hermes` memory marking G2 shipped.
4. **finishing-a-development-branch:** merge origin/master into the host branch first (shared-worktree rule), PR → master, GitHub-side merge.

---

## Self-Review

**Spec coverage:**
- Merger seam + Umbrella + merger() → Task 1 ✓
- MetaMergedInto label → Task 1 (const) + Task 2 (Sweep guard) ✓
- Merge loop (search → target filter → domain group → threshold → cap → merger) → Task 1 `Merge` ✓
- applyUmbrella (record + label + archive + link, lastSeen preserved) → Task 1 ✓
- Validation guard (empty key/body, <2 merged, key collision, key outside group) → Task 1 `validUmbrella` ✓
- Sweep skips mergedInto → Task 2 ✓
- Config triple + SetMerge + factory wiring → Task 1 (setter) + Task 3 (manifest+wiring) ✓
- Invariants: best-effort `_ = l.Merge(ctx)` (inv 2), reversible label/archive/link (inv 3), seam-only no contracts change (inv 1) — all in Task 1/2 ✓
- Testing list (no-op, disabled, below-threshold, happy path, guard, domain isolation, cap, target filter, sweep skip, best-effort) → Task 1 + Task 2 tests ✓

**Placeholder scan:** none — all steps carry full code and exact commands.

**Type consistency:** `Merge`/`mergeEligible`/`applyUmbrella`/`validUmbrella`/`SetMerge`/`merger` signatures identical across tasks; `MetaMergedInto`, `defaultMergeMax` defined once in Task 1 and consumed by name in Tasks 2/3; fake `mergeMem`/`fakeMerger` defined once (Task 1) and reused (Tasks 2/3); `stale()`/`learnerWith()` helpers defined in Task 1. Test helper `hour` vs `time.Hour` flagged in Task 2 Step 1 with a concrete resolution.
