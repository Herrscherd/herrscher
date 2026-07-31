# ★ Cross-Agent Skill Promotion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The curator copies a proven private skill (`agents/<a>/…`) into the shared project scope (`projects/<p>/…`) so every peer agent inherits it on their next `Context` — the "beyond Hermes" differentiator.

**Architecture:** A new best-effort `Learner.Promote(ctx)` pass runs at the tail of `Consolidate` after `Sweep` and `Merge`. Eligibility is a deterministic predicate over `Meta` stamps the graph already carries (`state`, `capturedAt`, `lastSeen`) — no LLM, no new seam. A promotion writes a shared copy, links original→copy (`promoted-to`), and labels the original with `MetaPromotedTo` (a do-not-repeat marker; the original stays live and private). Merge is taught to skip promoted originals so a private original and its shared copy are never re-fused.

**Tech Stack:** Go; module `github.com/Herrscherd/herrscher-orchestrator`; ports from `github.com/Herrscherd/herrscher-contracts` (already-released v0.2.9). Fake-`Memory` unit tests. go.work overlay for host cross-module dev.

## Global Constraints

- **Repos:** `herrscher-orchestrator` ONLY. NO `herrscher-contracts`, NO `herrscher-obsidian-memory` change. Host `go.mod` bump + README only.
- **Release footprint (OVERRIDES the spec's "v0.1.13"):** orchestrator → **v0.1.15** (current published tag is v0.1.14). Host `go.mod`: bump orchestrator **v0.1.14 → v0.1.15**.
- **Three invariants (non-negotiable):** (1) Ports only, policy not engine — route through `contracts.Memory`/`RecordShared`/`Links`, no new port method, no new engine, reuse existing Meta stamps; (2) Learning never breaks a turn — `Promote` is `_ = l.Promote(ctx)` at the Consolidate tail, error recorded internally, never propagated; (3) Reversible over destructive — the private original is never deleted/archived/rewritten beyond one label + an untouched `lastSeen`; the shared copy is a new node.
- **Fourth invariant (this slice):** Merge/promotion never re-fuse — `mergeEligible` skips any node bearing `Meta[MetaPromotedTo]`, mirroring its `MetaMergedInto` skip.
- **Config default OFF:** `promote-min-age-days` default `0` disables the pass cleanly (mirrors `merge-min-nodes`).
- **No `promote-target` axis, no counter Meta field, no demote command, no cross-project promotion** (YAGNI — spec §Out of scope).
- **CI gates to preserve (orchestrator):** `gofmt -l` clean, `go vet ./...`, `go build ./...`, `go test -race ./...`. Verify with `GOWORK=off` against real tags before release.
- **Git/network ops (tag, push, `go get`) are the main agent's — never a subagent's.** Subagents commit locally only.
- **Label constant naming:** `MetaPromotedTo = "promotedTo"` (mirrors `MetaMergedInto = "mergedInto"`). Informational copy-provenance key on the shared copy: `"promotedFrom"`.
- **`lastSeen` discipline:** any state-only re-`Record` of the original MUST re-supply the existing `lastSeen` so the per-write obsidian stamp does not reset the very age basis the rule depends on (same discipline as `Sweep`/`applyUmbrella`).

---

### Task 1: Eligibility predicate + label + config field/setter

**Files:**
- Create: `promote.go`
- Modify: `learner.go` (add one field to the `Learner` struct)
- Test: `promote_test.go`

**Interfaces:**
- Consumes: `contracts.Node`, `contracts.MetaState`, `contracts.StateActive`, `contracts.StateStale`, `contracts.StateArchived` (from contracts); `MetaMergedInto` (existing, `merge.go`); `Learner` struct + `l.now func() time.Time` (existing, `Curator`).
- Produces: `const MetaPromotedTo = "promotedTo"`; `Learner.promoteMinAge time.Duration` field; `func (l *Learner) SetPromote(minAge time.Duration)`; `func (l *Learner) promoteEligible(n contracts.Node, now time.Time) bool`.

- [ ] **Step 1: Add the `promoteMinAge` field to the `Learner` struct**

In `learner.go`, in the `Learner` struct definition, after the `mergeMin/mergeMax/mergeTarget` block (and its comment), add:

```go
	// promoteMinAge configures the ★ cross-agent promotion pass (Learner.Promote):
	// the minimum a private node's lastSeen must exceed its capturedAt before the
	// node is eligible to be copied into the shared project scope. <=0 disables the
	// pass (opt-in, default off); set via SetPromote.
	promoteMinAge time.Duration
```

Add `"time"` to `learner.go`'s import block if it is not already present (it is not — check and add).

- [ ] **Step 2: Write the failing test for `promoteEligible`**

Create `promote_test.go`:

```go
package orchestrator

import (
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts"
)

// promoNode builds a private node with the given state and a capturedAt/lastSeen
// gap of ageDays (lastSeen = capturedAt + ageDays).
func promoNode(key, state string, ageDays int) contracts.Node {
	captured := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	last := captured.Add(time.Duration(ageDays) * 24 * time.Hour)
	m := map[string]string{
		"capturedAt":           captured.Format(time.RFC3339),
		contracts.MetaLastSeen: last.Format(time.RFC3339),
	}
	if state != "" {
		m[contracts.MetaState] = state
	}
	return contracts.Node{Key: key, Meta: m}
}

func TestPromoteEligible(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	l := NewLearner(nil, "s", contracts.MemoryScope{}, plainExt{}, "", 0)
	l.SetPromote(10 * 24 * time.Hour) // 10-day bar

	cases := []struct {
		name string
		node contracts.Node
		want bool
	}{
		{"active old enough", promoNode("agents/a/skills/x", contracts.StateActive, 20), true},
		{"empty-state old enough", promoNode("agents/a/skills/x", "", 20), true},
		{"too young", promoNode("agents/a/skills/x", contracts.StateActive, 3), false},
		{"exactly at bar", promoNode("agents/a/skills/x", contracts.StateActive, 10), true},
		{"stale excluded", promoNode("agents/a/skills/x", contracts.StateStale, 20), false},
		{"archived excluded", promoNode("agents/a/skills/x", contracts.StateArchived, 20), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := l.promoteEligible(c.node, now); got != c.want {
				t.Errorf("promoteEligible = %v, want %v", got, c.want)
			}
		})
	}
}

func TestPromoteEligibleTerminalLabels(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	l := NewLearner(nil, "s", contracts.MemoryScope{}, plainExt{}, "", 0)
	l.SetPromote(10 * 24 * time.Hour)

	merged := promoNode("agents/a/skills/x", contracts.StateActive, 20)
	merged.Meta[MetaMergedInto] = "agents/a/u"
	if l.promoteEligible(merged, now) {
		t.Error("a merged-away node must never be eligible")
	}
	promoted := promoNode("agents/a/skills/x", contracts.StateActive, 20)
	promoted.Meta[MetaPromotedTo] = "projects/p/skills/x"
	if l.promoteEligible(promoted, now) {
		t.Error("an already-promoted node must never be re-eligible")
	}
}

func TestPromoteEligibleBadTimestamps(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	l := NewLearner(nil, "s", contracts.MemoryScope{}, plainExt{}, "", 0)
	l.SetPromote(10 * 24 * time.Hour)

	noCaptured := contracts.Node{Key: "agents/a/x", Meta: map[string]string{
		contracts.MetaLastSeen: now.Format(time.RFC3339),
	}}
	if l.promoteEligible(noCaptured, now) {
		t.Error("missing capturedAt must fail eligibility, not panic")
	}
	badStamp := contracts.Node{Key: "agents/a/x", Meta: map[string]string{
		"capturedAt":           "not-a-time",
		contracts.MetaLastSeen: now.Format(time.RFC3339),
	}}
	if l.promoteEligible(badStamp, now) {
		t.Error("unparseable capturedAt must fail eligibility")
	}
}

func TestSetPromoteDisabledByDefault(t *testing.T) {
	l := NewLearner(nil, "s", contracts.MemoryScope{}, plainExt{}, "", 0)
	if l.promoteMinAge != 0 {
		t.Fatalf("promoteMinAge = %v, want 0 (disabled by default)", l.promoteMinAge)
	}
}
```

Run: `GOWORK=off go test ./... -run 'TestPromoteEligible|TestSetPromote' -v`
Expected: FAIL — `undefined: MetaPromotedTo`, `l.SetPromote`, `l.promoteEligible`.

- [ ] **Step 3: Implement the label, setter, and predicate**

Create `promote.go`:

```go
package orchestrator

import (
	"time"

	"github.com/Herrscherd/herrscher-contracts"
)

// MetaPromotedTo, when set on a private node, names the Key of the shared copy
// it was promoted to. It is a terminal marker on the ORIGINAL: the original is
// kept (reversible, still private, still usable by its own agent) but is never
// re-promoted. Orchestrator-internal — obsidian stores Meta generically, so no
// contracts change is needed.
const MetaPromotedTo = "promotedTo"

// SetPromote configures the ★ cross-agent promotion pass. minAge <= 0 disables
// the pass (default).
func (l *Learner) SetPromote(minAge time.Duration) {
	l.promoteMinAge = minAge
}

// promoteEligible reports whether private node n has proven itself enough to be
// copied into the shared project scope. The rule is deterministic over Meta
// stamps already on the node (no counter field exists): active (or implicit
// active), not merged-away, not already promoted, with both age stamps present
// and parseable, and a lastSeen that has advanced past capturedAt by at least
// promoteMinAge (i.e. the node was re-observed, not written once and left).
//
// now is accepted for signature symmetry with Sweep/NextState but unused: age
// is measured between the two stamps, not against the wall clock. It is kept so
// a future refinement (e.g. "also require now-lastSeen recency") needs no
// call-site churn.
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

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOWORK=off go test ./... -run 'TestPromoteEligible|TestSetPromote' -v`
Expected: PASS (all subtests).

- [ ] **Step 5: gofmt/vet and commit**

```bash
GOWORK=off gofmt -w promote.go promote_test.go learner.go
GOWORK=off go vet ./...
git add promote.go promote_test.go learner.go
git commit -m "feat(memory): ★ promotion eligibility predicate + label + config field"
```

---

### Task 2: Shared-copy key derivation + `applyPromotion` + `Promote` pass

**Files:**
- Modify: `promote.go`
- Test: `promote_test.go`

**Interfaces:**
- Consumes: `MetaPromotedTo`, `promoteEligible` (Task 1); `contracts.RecordShared(ctx, mem, scope, node)`, `contracts.ProjectKey`, `contracts.Query{}`, `contracts.Memory.Search/Record/Links`; `l.scope contracts.MemoryScope`, `l.mem contracts.Memory`, `l.now func() time.Time` (existing).
- Produces: `func promotedKey(project contracts.ProjectKey, agentKey string) string`; `func cloneMeta(m map[string]string) map[string]string`; `func (l *Learner) applyPromotion(ctx context.Context, n contracts.Node) error`; `func (l *Learner) Promote(ctx context.Context) error`.

- [ ] **Step 1: Write the failing tests for `promotedKey` and `Promote`**

Append to `promote_test.go` (the `mergeMem` fake and `plainExt`/`stale` helpers live in `merge_test.go`, same package — reuse them; note `mergeMem.Search` returns its `nodes` slice verbatim and `Record`/`Links` capture writes):

```go
func TestPromotedKey(t *testing.T) {
	got := promotedKey(contracts.ProjectKey("projects/neublox"), "agents/roblox-dev/skills/retry-http")
	if want := "projects/neublox/skills/retry-http"; got != want {
		t.Errorf("promotedKey = %q, want %q", got, want)
	}
}

// promoteLearner builds a Learner over mem with a project+agent scope and the
// promotion bar set.
func promoteLearner(mem contracts.Memory, minAgeDays int) *Learner {
	l := NewLearner(mem, "s", contracts.MemoryScope{
		Project: contracts.ProjectKey("projects/p"),
		Agent:   contracts.AgentKey("agents/a"),
	}, plainExt{}, "", 0)
	l.SetPromote(time.Duration(minAgeDays) * 24 * time.Hour)
	return l
}

func TestPromoteDisabledIsNoop(t *testing.T) {
	mem := &mergeMem{nodes: []contracts.Node{promoNode("agents/a/skills/x", contracts.StateActive, 20)}}
	l := promoteLearner(mem, 0) // disabled
	if err := l.Promote(context.Background()); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if len(mem.records) != 0 || len(mem.links) != 0 {
		t.Fatalf("disabled promotion must be a no-op; got %d records %d links", len(mem.records), len(mem.links))
	}
}

func TestPromoteNoScopeIsNoop(t *testing.T) {
	mem := &mergeMem{nodes: []contracts.Node{promoNode("agents/a/skills/x", contracts.StateActive, 20)}}
	l := NewLearner(mem, "s", contracts.MemoryScope{Agent: contracts.AgentKey("agents/a")}, plainExt{}, "", 0) // no Project
	l.SetPromote(10 * 24 * time.Hour)
	if err := l.Promote(context.Background()); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if len(mem.records) != 0 {
		t.Fatalf("no project scope must be a no-op; got %d records", len(mem.records))
	}
}

func TestPromoteHappyPath(t *testing.T) {
	orig := promoNode("agents/a/skills/x", contracts.StateActive, 20)
	orig.Body = "retry with backoff"
	mem := &mergeMem{nodes: []contracts.Node{orig}}
	l := promoteLearner(mem, 10)
	if err := l.Promote(context.Background()); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	// shared copy written under the project scope
	var copy *contracts.Node
	for i := range mem.records {
		if mem.records[i].Key == "projects/p/skills/x" {
			copy = &mem.records[i]
		}
	}
	if copy == nil {
		t.Fatal("shared copy not recorded under projects/p/skills/x")
	}
	if copy.Body != "retry with backoff" {
		t.Errorf("copy body = %q, want the original body", copy.Body)
	}
	if copy.Meta["promotedFrom"] != "agents/a/skills/x" {
		t.Errorf("copy promotedFrom = %q, want the original key", copy.Meta["promotedFrom"])
	}
	if copy.Meta[MetaPromotedTo] != "" {
		t.Errorf("copy must not itself carry promotedTo: %q", copy.Meta[MetaPromotedTo])
	}
	// original labeled (last record for that key)
	var labeled *contracts.Node
	for i := range mem.records {
		if mem.records[i].Key == "agents/a/skills/x" {
			labeled = &mem.records[i]
		}
	}
	if labeled == nil || labeled.Meta[MetaPromotedTo] != "projects/p/skills/x" {
		t.Fatalf("original not labeled with promotedTo: %+v", labeled)
	}
	// original state untouched, lastSeen preserved
	if labeled.Meta[contracts.MetaState] != contracts.StateActive {
		t.Errorf("original state changed to %q", labeled.Meta[contracts.MetaState])
	}
	if labeled.Meta[contracts.MetaLastSeen] != orig.Meta[contracts.MetaLastSeen] {
		t.Errorf("original lastSeen bumped: %q", labeled.Meta[contracts.MetaLastSeen])
	}
	// link original -> copy
	var linked bool
	for _, ln := range mem.links {
		if ln == [3]string{"agents/a/skills/x", "projects/p/skills/x", "promoted-to"} {
			linked = true
		}
	}
	if !linked {
		t.Errorf("missing promoted-to link: %+v", mem.links)
	}
}

func TestPromoteScopeIsolation(t *testing.T) {
	// A different agent's private node must never be scanned/promoted.
	mem := &mergeMem{nodes: []contracts.Node{promoNode("agents/other/skills/x", contracts.StateActive, 20)}}
	l := promoteLearner(mem, 10)
	if err := l.Promote(context.Background()); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if len(mem.records) != 0 {
		t.Fatalf("another agent's node was promoted; got %d records", len(mem.records))
	}
}

func TestPromoteIdempotentRerun(t *testing.T) {
	mem := &mergeMem{nodes: []contracts.Node{promoNode("agents/a/skills/x", contracts.StateActive, 20)}}
	l := promoteLearner(mem, 10)
	if err := l.Promote(context.Background()); err != nil {
		t.Fatalf("Promote 1: %v", err)
	}
	// The fake's Search returns the ORIGINAL nodes slice, not the labeled write;
	// simulate the durable label by replacing the node with its labeled version.
	for i := range mem.nodes {
		if mem.nodes[i].Key == "agents/a/skills/x" {
			mem.nodes[i].Meta[MetaPromotedTo] = "projects/p/skills/x"
		}
	}
	before := len(mem.records)
	if err := l.Promote(context.Background()); err != nil {
		t.Fatalf("Promote 2: %v", err)
	}
	if len(mem.records) != before {
		t.Fatalf("re-run promoted again: records %d -> %d", before, len(mem.records))
	}
}

func TestPromoteBestEffortOnRecordError(t *testing.T) {
	// Two eligible nodes; the shared copy of the first fails to write. The second
	// must still be promoted, and Promote returns the first error.
	mem := &mergeMem{
		nodes: []contracts.Node{
			promoNode("agents/a/skills/x", contracts.StateActive, 20),
			promoNode("agents/a/skills/y", contracts.StateActive, 20),
		},
		recErrOn: "projects/p/skills/x",
	}
	l := promoteLearner(mem, 10)
	if err := l.Promote(context.Background()); err == nil {
		t.Fatal("expected the failing copy's error to surface")
	}
	var sawY bool
	for _, r := range mem.records {
		if r.Key == "projects/p/skills/y" {
			sawY = true
		}
	}
	if !sawY {
		t.Fatal("sibling promotion was skipped after the first node's failure")
	}
}
```

Add the imports `context` to the top of `promote_test.go` (it already imports `testing`, `time`, `contracts` from Task 1 — add `"context"`).

Run: `GOWORK=off go test ./... -run 'TestPromote|TestPromotedKey' -v`
Expected: FAIL — `undefined: promotedKey`, `l.Promote`.

- [ ] **Step 2: Implement `promotedKey`, `cloneMeta`, `applyPromotion`, and `Promote`**

Append to `promote.go` and add `"context"` and `"strings"` to its import block:

```go
// promotedKey derives the stable shared Key for a promoted private node from the
// project key and the tail of the private key, e.g.
// ("projects/neublox", "agents/roblox-dev/skills/retry-http") ->
// "projects/neublox/skills/retry-http". A pure function of its inputs, so a
// retry after a mid-promotion crash recomputes the same Key and upserts rather
// than duplicating the shared copy.
func promotedKey(project contracts.ProjectKey, agentKey string) string {
	_, tail, _ := strings.Cut(agentKey, "/") // "agents/<agent>/<tail>" -> "<tail>"
	return string(project) + "/" + tail
}

// cloneMeta returns a shallow copy of m (or an empty map for nil), so mutating
// the copy's Meta cannot alias the original node's map.
func cloneMeta(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// applyPromotion writes a shared copy of a proven private node n under the
// project scope, links the original to it, and labels the original so it is
// never re-promoted. Reversible: the original is kept, untouched apart from the
// label; nothing is archived or deleted. Write-then-label order mirrors
// applyUmbrella so a crash between the two leaves the original still eligible
// for a safe retry (Record upserts by Key) rather than labeled-but-copyless.
func (l *Learner) applyPromotion(ctx context.Context, n contracts.Node) error {
	dup := n
	dup.Key = promotedKey(l.scope.Project, n.Key)
	dup.Meta = cloneMeta(n.Meta)
	delete(dup.Meta, MetaPromotedTo) // the copy is not itself "promoted"
	dup.Meta["promotedFrom"] = n.Key
	if err := contracts.RecordShared(ctx, l.mem, l.scope, dup); err != nil {
		return err
	}
	if err := l.mem.Links(ctx, n.Key, dup.Key, "promoted-to"); err != nil {
		return err // label not yet set: a retry re-attempts the whole write
	}
	if n.Meta == nil {
		n.Meta = map[string]string{}
	}
	n.Meta[MetaPromotedTo] = dup.Key
	// Re-supply lastSeen implicitly by re-Recording n unchanged apart from the
	// label: n already carries its lastSeen from Search, so this state-only write
	// does not reset the age the promotion rule depends on (same discipline as
	// Sweep/applyUmbrella).
	return l.mem.Record(ctx, n)
}

// Promote copies each eligible private skill of this agent's own scope into the
// shared project scope, so peer agents inherit it via RecallScoped. Best-effort
// and idempotent: disabled (promoteMinAge<=0), no project/agent scope, or nil
// Memory all yield a clean no-op; a per-node write failure is recorded as the
// first error but never aborts the rest of the pass.
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

Note: the spec used the identifier `copy` for the local; this plan renames it `dup` because `copy` is a Go builtin and shadowing it triggers a lint smell. Behaviour is identical.

- [ ] **Step 3: Run the tests to verify they pass**

Run: `GOWORK=off go test ./... -run 'TestPromote|TestPromotedKey' -v`
Expected: PASS (all cases).

- [ ] **Step 4: gofmt/vet and commit**

```bash
GOWORK=off gofmt -w promote.go promote_test.go
GOWORK=off go vet ./...
git add promote.go promote_test.go
git commit -m "feat(memory): ★ Promote pass — shared-copy write, link, and label"
```

---

### Task 3: Consolidate wiring + Merge exclusion of promoted originals

**Files:**
- Modify: `learner.go` (Consolidate tail: add `_ = l.Promote(ctx)` after Merge)
- Modify: `merge.go` (`mergeEligible` skips `MetaPromotedTo`)
- Test: `promote_test.go`

**Interfaces:**
- Consumes: `Learner.Promote` (Task 2), `Learner.Merge`/`Learner.Sweep` (existing), `mergeEligible` (existing, `merge.go`), `MetaPromotedTo` (Task 1).
- Produces: no new symbols — behavioural wiring only.

- [ ] **Step 1: Write the failing tests**

Append to `promote_test.go`:

```go
// countingPromoMem records the order of key-writes so we can assert the pass
// ordering Sweep -> Merge -> Promote.
type orderMem struct {
	mergeMem
	order []string
}

func (m *orderMem) Record(ctx context.Context, n contracts.Node) error {
	m.order = append(m.order, n.Key)
	return m.mergeMem.Record(ctx, n)
}

func TestConsolidateRunsPromoteAfterMerge(t *testing.T) {
	// A single eligible private node; with a nil extractor Extract yields nothing,
	// but Sweep/Merge/Promote still run at the Consolidate tail. Promote must fire
	// and produce the shared copy.
	mem := &mergeMem{nodes: []contracts.Node{promoNode("agents/a/skills/x", contracts.StateActive, 20)}}
	l := promoteLearner(mem, 10)
	if err := l.Consolidate(context.Background()); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}
	var promoted bool
	for _, r := range mem.records {
		if r.Key == "projects/p/skills/x" {
			promoted = true
		}
	}
	if !promoted {
		t.Fatal("Consolidate did not run Promote (no shared copy written)")
	}
}

func TestMergeSkipsPromotedOriginal(t *testing.T) {
	// A promoted original in a mergeable domain group must be excluded from merge
	// candidacy so it is never re-fused with (or instead of) its shared copy.
	a := stale("agents/a/skills/x", "http")
	a.Meta[MetaPromotedTo] = "projects/p/skills/x"
	b := stale("agents/a/skills/y", "http")
	mem := &mergeMem{nodes: []contracts.Node{a, b}}
	f := &fakeMerger{result: nil} // capture what it is handed
	l := learnerWith(mem, f, 2, 40, "stale")
	if err := l.Merge(context.Background()); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	// With the promoted original excluded, only 1 candidate remains in the group,
	// which is below the min-2 threshold, so the merger is never called.
	if len(f.calls) != 0 {
		t.Fatalf("merger was called with a promoted original in the group: %+v", f.calls)
	}
}
```

Run: `GOWORK=off go test ./... -run 'TestConsolidateRunsPromoteAfterMerge|TestMergeSkipsPromotedOriginal' -v`
Expected: FAIL — Consolidate does not yet call Promote; `mergeEligible` does not yet skip promoted originals (merger gets called).

- [ ] **Step 2: Wire `Promote` into `Consolidate`**

In `learner.go`'s `Consolidate`, after the `_ = l.Merge(ctx)` line and its comment block, add:

```go
	// Best-effort cross-agent promotion after the merge (opt-in via SetPromote;
	// a no-op when disabled or when nothing is eligible). Promote must see the
	// post-sweep, post-merge state so a freshly-archived merge original is never
	// promoted. A promotion error must never propagate out of Consolidate
	// (invariant: learning never breaks a turn).
	_ = l.Promote(ctx)
```

This goes BEFORE the `_ = l.report(ctx)` line so a promotion that produced transitions… (note: Promote does not append to `l.transitions` — it is not a lifecycle state change and has no audit row; it appears only via the `promoted-to` graph link and the `promotedTo`/`promotedFrom` labels). Placement is: `Sweep → Merge → Promote → report`.

- [ ] **Step 3: Teach `mergeEligible` to skip promoted originals**

In `merge.go`, in `mergeEligible`, add the `MetaPromotedTo` guard alongside the existing state checks. The current function reads the state and switches on target; add the terminal-label guard at the top:

```go
func (l *Learner) mergeEligible(n contracts.Node) bool {
	if n.Meta[MetaPromotedTo] != "" {
		return false // a promoted original is settled: never re-fuse it with its shared copy
	}
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
```

(If `mergeEligible` already skips `MetaMergedInto` inside itself, keep that; per the current code the `MetaMergedInto` skip lives in the `Merge` loop / `Search`, not in `mergeEligible` — do NOT remove any existing guard, only ADD the `MetaPromotedTo` one.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOWORK=off go test ./... -run 'TestConsolidateRunsPromoteAfterMerge|TestMergeSkipsPromotedOriginal' -v`
Expected: PASS.

- [ ] **Step 5: Run the full package to confirm no regression**

Run: `GOWORK=off go test -race ./...`
Expected: PASS (all tests, race-clean).

- [ ] **Step 6: gofmt/vet and commit**

```bash
GOWORK=off gofmt -w learner.go merge.go promote_test.go
GOWORK=off go vet ./...
git add learner.go merge.go promote_test.go
git commit -m "feat(memory): ★ wire Promote into Consolidate + exclude promoted originals from merge"
```

---

### Task 4: Config surface (`promote-min-age-days`)

**Files:**
- Modify: `register.go`
- Test: `register_test.go` (create if absent) OR extend the existing manifest test

**Interfaces:**
- Consumes: `l.SetPromote` (Task 1), the existing `contracts.Manifest.Config` list and the Learner branch of the `Orchestrator` factory.
- Produces: a new `Setting{Key: "promote-min-age-days", ...}` in the manifest and a `l.SetPromote(...)` call in the Learner branch.

- [ ] **Step 1: Write the failing test**

Check whether a `register_test.go` exists (`ls register_test.go`). If it does, append; otherwise create it:

```go
package orchestrator

import (
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestManifestHasPromoteSetting(t *testing.T) {
	var found *contracts.Setting
	for _, p := range contracts.Plugins() {
		if p.Manifest.Category != contracts.CategoryOrchestrator {
			continue
		}
		for i := range p.Manifest.Config {
			if p.Manifest.Config[i].Key == "promote-min-age-days" {
				found = &p.Manifest.Config[i]
			}
		}
	}
	if found == nil {
		t.Fatal("manifest missing promote-min-age-days setting")
	}
	if found.Env != "MEMORY_PROMOTE_MIN_AGE_DAYS" {
		t.Errorf("env = %q, want MEMORY_PROMOTE_MIN_AGE_DAYS", found.Env)
	}
}
```

Note: confirm the enumerator is `contracts.Plugins()` — grep for how `extractor_registry_test.go` enumerates registered plugins (it references `p.Manifest.Kind`); reuse the exact accessor that test uses. If the accessor differs, match it verbatim rather than inventing `contracts.Plugins()`.

Run: `GOWORK=off go test ./... -run TestManifestHasPromoteSetting -v`
Expected: FAIL — setting not in manifest.

- [ ] **Step 2: Add the manifest entry and the setter wiring**

In `register.go`, add to the `Config: []contracts.Setting{...}` list, after the `report-prefix` entry:

```go
					{Key: "promote-min-age-days", Env: "MEMORY_PROMOTE_MIN_AGE_DAYS", Help: "days a private skill's lastSeen must exceed its capturedAt before the curator promotes it to the shared project scope; <=0 disables (default 0, off)", Required: false},
```

In the Learner branch, after `l.SetReport(reportEnabled, cfg.Get("report-prefix"))`, add:

```go
					promoteDays, _ := strconv.Atoi(cfg.Get("promote-min-age-days"))
					l.SetPromote(time.Duration(promoteDays) * 24 * time.Hour)
```

`register.go` already imports `strconv` and `time` — confirm both are present (they are).

- [ ] **Step 3: Run the test to verify it passes**

Run: `GOWORK=off go test ./... -run TestManifestHasPromoteSetting -v`
Expected: PASS.

- [ ] **Step 4: Full suite + gofmt/vet + go mod tidy check**

```bash
GOWORK=off gofmt -l .
GOWORK=off go vet ./...
GOWORK=off go build ./...
GOWORK=off go test -race ./...
GOWORK=off go mod tidy && git diff --exit-code go.mod go.sum
```
Expected: gofmt prints nothing; vet/build clean; all tests pass race-clean; `go mod tidy` leaves go.mod/go.sum unchanged (exit 0).

- [ ] **Step 5: Commit**

```bash
git add register.go register_test.go
git commit -m "feat(memory): ★ config surface — promote-min-age-days"
```

---

### Task 5: Release orchestrator v0.1.15, bump host, README (MAIN AGENT ONLY)

> **This task is executed by the main agent, not a subagent** — it performs git tag/push and `go get` (network) operations that subagents must not run. The SDD controller runs these steps directly after Task 4's review is clean.

**Files:**
- Modify (host): `go.mod`, `go.sum`, `README.md`
- Tag (orchestrator): `v0.1.15`

**Interfaces:**
- Consumes: all of Tasks 1–4 committed on `herrscher-orchestrator` master.
- Produces: published tag `v0.1.15`; host built against the real tag with the go.work overlay disabled.

- [ ] **Step 1: Confirm orchestrator master is clean and green vs real deps**

```bash
cd /home/shan/dev/herrscher-orchestrator
GOWORK=off gofmt -l . && GOWORK=off go vet ./... && GOWORK=off go test -race ./...
```
Expected: gofmt silent; vet clean; all tests pass.

- [ ] **Step 2: Tag and push (main agent, via rtk proxy git)**

```bash
cd /home/shan/dev/herrscher-orchestrator
git tag v0.1.15
git push origin master --tags
```
Expected: `master -> master`, `[new tag] v0.1.15 -> v0.1.15`.

- [ ] **Step 3: Bump the host dependency against the real tag**

From the host worktree root:

```bash
GOWORK=off go get github.com/Herrscherd/herrscher-orchestrator@v0.1.15
GOWORK=off go mod tidy
```
Expected: `upgraded … v0.1.14 => v0.1.15`; go.mod/go.sum updated.

- [ ] **Step 4: Add the README "Cross-agent promotion" paragraph**

In `README.md`, in the "Learning (the write side)" section (after the merge/report material), add a paragraph describing ★:

> **Cross-agent promotion.** When a private skill (`agents/<agent>/…`) has proven itself — re-observed so its `lastSeen` has advanced past its `capturedAt` by at least `promote-min-age-days` (env `MEMORY_PROMOTE_MIN_AGE_DAYS`; default `0`, off) — the curator copies it into the shared project scope (`projects/<project>/…`) so every peer agent inherits it on their next turn. The private original is kept, live, and unchanged apart from a `promotedTo` label; the shared copy is a new node linked back via `promoted-to`. The pass is deterministic (no LLM), best-effort (never breaks a turn), and idempotent (a node is promoted at most once).

Match the existing README heading style and surrounding prose. Read the current "Learning" section first to place it correctly and mirror the tone.

- [ ] **Step 5: Full host verification vs the real tag**

```bash
GOWORK=off gofmt -l .
GOWORK=off go vet ./...
GOWORK=off go build ./...
GOWORK=off go test -race ./...
GOWORK=off go mod tidy && git diff --exit-code go.mod go.sum
```
Expected: gofmt silent; vet/build clean; full host suite passes race-clean; `go mod tidy` clean (exit 0).

- [ ] **Step 6: Commit the host bump + README on the branch and push**

```bash
git add go.mod go.sum README.md
git commit -m "feat(memory): ★ cross-agent promotion — bump orchestrator v0.1.15 + README"
git push
```
Expected: branch pushed; PR CI re-runs green.

---

## Self-Review

**1. Spec coverage** (spec §-by-§):
- §1 deterministic rule, no seam → Task 1 (predicate, no interface). ✅
- §2 eligibility rule (5 clauses) → Task 1 `promoteEligible` + tests (state, merged, promoted, timestamps, age). ✅
- §3 `MetaPromotedTo` label → Task 1 const. ✅
- §4 `Promote` + `applyPromotion` + `promotedKey` → Task 2. ✅
- §5 reversibility (original kept, only labeled, lastSeen preserved) → Task 2 `applyPromotion` + `TestPromoteHappyPath` assertions. ✅
- §6 ordering (Sweep→Merge→Promote) + Sweep unchanged → Task 3 + `TestConsolidateRunsPromoteAfterMerge`. ✅
- §Interaction-with-G2 (`mergeEligible` skips `MetaPromotedTo`) → Task 3 + `TestMergeSkipsPromotedOriginal`. ✅
- §7 config (one setting + `SetPromote` + register wiring) → Task 1 (setter) + Task 4 (manifest/wiring). ✅
- Idempotence → Task 2 `TestPromoteIdempotentRerun`, `promotedKey` purity `TestPromotedKey`. ✅
- Testing list (§Testing) → mapped across Task 1–3 tests (disabled, no-scope, below-age, happy, stale/archived excluded, merged excluded, idempotent, scope isolation, bad timestamps, best-effort, ordering). ✅
- Release footprint → Task 5, with the v0.1.15 override applied. ✅

**2. Placeholder scan:** No TBD/TODO; every code step shows full code; every run step shows the command + expected result. ✅

**3. Type consistency:** `MetaPromotedTo` ("promotedTo"), `promoteEligible(n, now)`, `promotedKey(project, agentKey)`, `applyPromotion(ctx, n)`, `Promote(ctx)`, `SetPromote(minAge)`, field `promoteMinAge` — used identically across Tasks 1–4. `dup` (not `copy`) used consistently in Task 2. Fake `mergeMem`/`fakeMerger`/`stale`/`plainExt`/`learnerWith` reused from `merge_test.go` (same package). ✅

**Open verification the implementer must confirm (flagged, not a gap):**
- Task 4's plugin enumerator accessor: the plan uses `contracts.Plugins()` but instructs the implementer to match whatever `extractor_registry_test.go` actually uses. If they differ, match the existing test verbatim.
- Task 3 Step 3 assumes `mergeEligible` does not already contain a `MetaPromotedTo` guard (it does not, per the current `merge.go`); the implementer adds it without removing any existing guard.
