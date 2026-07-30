# G3 — Staleness State Machine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every memory node a deterministic, time-based lifecycle `active → stale → archived` (with automatic reactivation on re-observation), driven by a pure function in contracts and a best-effort sweep in the orchestrator.

**Architecture:** A pure `NextState` transition in `herrscher-contracts` (no clock read, no LLM); obsidian stamps `lastSeen` on every write and hides archived nodes from ordinary Search/Recall; the orchestrator `Curator` gains an injectable clock + windows + a `Sweep` that re-derives and persists state, called best-effort at the end of `Learner.Consolidate`; env config is read in the orchestrator plugin's own `register.go` (mirroring obsidian's `OBSIDIAN_NODE_BUDGET`).

**Tech Stack:** Go, three modules (`herrscher-contracts`, `herrscher-obsidian-memory`, `herrscher-orchestrator`) + host, `go.work` overlay for cross-module dev.

## Global Constraints

- **Invariant 1 — Ports only, policy not engine:** staleness is a pure function in contracts plus a sweep over the existing `Memory` port. No new storage engine.
- **Invariant 2 — Learning never breaks a turn:** `Sweep` is best-effort; `Learner.Consolidate` must never return an error caused by `Sweep`.
- **Invariant 3 — Reversible over destructive:** G3 only *labels* state. Nothing is moved or deleted. Archived nodes stay on disk and reactivate on re-observation.
- **Additive only:** every contracts/obsidian/orchestrator change is backward-compatible (new file, new fields, new methods). No signature of an existing exported symbol changes.
- **Defaults:** stale after **30 days**, archived after **90 days**. Env `AGENT_STALE_DAYS` / `AGENT_ARCHIVE_DAYS` override (integer days). A window `<= 0` disables that transition.
- **Age basis:** `age = now.Sub(lastSeen)`; state uses `>=` at the boundary (higher state wins). Current state is NOT an input to `NextState` (hysteresis-free).
- **Timestamps:** RFC3339, UTC (`m.now().UTC().Format(time.RFC3339)`), matching the existing `capturedAt` convention.
- **Subagents must NOT run any git tag / push / network / `go get` / `go mod tidy` command.** Those are main-agent-only (see Release Wiring). Subagents implement + `go test` against the `go.work` overlay only.
- **Release order (dependency order):** contracts v0.2.9 → obsidian v0.2.7 + orchestrator v0.1.9 → host go.mod bump. Every module imported by the public host must be a public tag or master CI goes red.

---

## File Structure

- `herrscher-contracts/state.go` — **create**: state constants, Meta-key constants, `NextState`.
- `herrscher-contracts/state_test.go` — **create**: `NextState` table test.
- `herrscher-contracts/memory.go` — **modify**: add `Query.IncludeArchived bool`.
- `herrscher-obsidian-memory/memory.go` — **modify**: stamp `lastSeen` in `writeNode`; exclude archived in `matchesQuery` + `Recall`.
- `herrscher-obsidian-memory/memory_test.go` — **modify**: lastSeen + archived-exclusion tests.
- `herrscher-orchestrator/orchestrator.go` — **modify**: `Curator` clock + windows + defaults + `SetStaleness`.
- `herrscher-orchestrator/sweep.go` — **create**: `Curator.Sweep`.
- `herrscher-orchestrator/sweep_test.go` — **create**: sweep transition + no-churn + reactivation tests.
- `herrscher-orchestrator/learner.go` — **modify**: call `Sweep` best-effort at end of `Consolidate`.
- `herrscher-orchestrator/register.go` — **modify**: Manifest `Config` + factory `SetStaleness` wiring.
- `herrscher-orchestrator/register_test.go` — **modify/create**: env → staleness wiring test.
- host `go.mod` — **modify** (main-agent, Release Wiring): bump the three deps.

---

## Task 0: Dev overlay (`go.work`)

**Files:**
- Create: `<host>/go.work` (untracked — never committed)

**Interfaces:**
- Produces: a cross-module build/test overlay so every later task compiles the three local checkouts against each other before any tag exists.

- [ ] **Step 1: Create the overlay**

From the host worktree root (`/home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review`):

```bash
cat > go.work <<'EOF'
go 1.23

use (
	.
	/home/shan/dev/herrscher-contracts
	/home/shan/dev/herrscher-obsidian-memory
	/home/shan/dev/herrscher-orchestrator
)
EOF
```

(Match the `go` directive to the host `go.mod`'s version if it differs from 1.23.)

- [ ] **Step 2: Verify the overlay builds**

Run: `cd <host> && go build ./...`
Expected: builds clean (overlay resolves the three local modules).

- [ ] **Step 3: Confirm it is untracked**

Run: `git -C <host> status --porcelain go.work`
Expected: `?? go.work` (never staged; it is removed at release).

**No commit** — `go.work` is dev-only scaffolding.

---

## Task 1: contracts — `NextState` + state vocabulary + `Query.IncludeArchived`

**Files:**
- Create: `/home/shan/dev/herrscher-contracts/state.go`
- Create: `/home/shan/dev/herrscher-contracts/state_test.go`
- Modify: `/home/shan/dev/herrscher-contracts/memory.go` (add `IncludeArchived` to `Query`)

**Interfaces:**
- Produces:
  - `const StateActive="active"; StateStale="stale"; StateArchived="archived"`
  - `const MetaState="state"; MetaLastSeen="lastSeen"`
  - `func NextState(lastSeen, now time.Time, staleAfter, archiveAfter time.Duration) string`
  - `Query.IncludeArchived bool` (consumed by obsidian in Task 3)

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-contracts/state_test.go`:

```go
package contracts

import (
	"testing"
	"time"
)

func TestNextState(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	const stale = 30 * 24 * time.Hour
	const archive = 90 * 24 * time.Hour

	cases := []struct {
		name         string
		lastSeen     time.Time
		stale, arch  time.Duration
		want         string
	}{
		{"fresh", now.Add(-time.Hour), stale, archive, StateActive},
		{"just before stale", now.Add(-(stale - time.Minute)), stale, archive, StateActive},
		{"exactly at stale", now.Add(-stale), stale, archive, StateStale},
		{"between stale and archive", now.Add(-60 * 24 * time.Hour), stale, archive, StateStale},
		{"exactly at archive", now.Add(-archive), stale, archive, StateArchived},
		{"well past archive", now.Add(-365 * 24 * time.Hour), stale, archive, StateArchived},
		{"stale disabled", now.Add(-60 * 24 * time.Hour), 0, archive, StateActive},
		{"archive disabled stays stale", now.Add(-365 * 24 * time.Hour), stale, 0, StateStale},
		{"both disabled", now.Add(-365 * 24 * time.Hour), 0, 0, StateActive},
		{"reactivation: recent lastSeen", now.Add(-time.Minute), stale, archive, StateActive},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NextState(c.lastSeen, now, c.stale, c.arch); got != c.want {
				t.Fatalf("NextState = %q, want %q", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run TestNextState`
Expected: FAIL (undefined: NextState / StateActive / …).

- [ ] **Step 3: Write the implementation**

Create `/home/shan/dev/herrscher-contracts/state.go`:

```go
package contracts

import "time"

// Node lifecycle states, stored in Node.Meta[MetaState]. An absent value is
// treated as StateActive.
const (
	StateActive   = "active"
	StateStale    = "stale"
	StateArchived = "archived"
)

// Reserved Meta keys for the staleness machine.
const (
	MetaState    = "state"
	MetaLastSeen = "lastSeen"
)

// NextState derives a node's lifecycle state purely from how long ago it was
// last seen. age = now.Sub(lastSeen). A duration <= 0 disables that step:
// staleAfter <= 0 means nodes never become stale; archiveAfter <= 0 means they
// never become archived. When both are set, archiveAfter should exceed
// staleAfter; if archiveAfter <= staleAfter, archival still wins once its
// threshold is crossed. The current state is intentionally not an input:
// transitions (including reactivation) depend only on age, so the function is
// total and hysteresis-free.
func NextState(lastSeen, now time.Time, staleAfter, archiveAfter time.Duration) string {
	age := now.Sub(lastSeen)
	if archiveAfter > 0 && age >= archiveAfter {
		return StateArchived
	}
	if staleAfter > 0 && age >= staleAfter {
		return StateStale
	}
	return StateActive
}
```

- [ ] **Step 4: Add `IncludeArchived` to `Query`**

In `/home/shan/dev/herrscher-contracts/memory.go`, add the field to the `Query` struct (after `Ranked bool`, inside the struct):

```go
	// IncludeArchived includes nodes whose Meta[MetaState] == StateArchived in the
	// result. Default false: archived nodes are hidden from ordinary Search/Recall.
	// The curator sweep sets it true so it can still reach (and reactivate) them.
	IncludeArchived bool
```

- [ ] **Step 5: Run tests to verify pass**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./...`
Expected: PASS (all, including the existing suite).

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher-contracts
git add state.go state_test.go memory.go
git commit -m "feat(memory): NextState staleness transition + Query.IncludeArchived (G3)"
```

---

## Task 2: obsidian — stamp `lastSeen` on every write

**Files:**
- Modify: `/home/shan/dev/herrscher-obsidian-memory/memory.go` (`writeNode`, after the `capturedAt` block ~line 165)
- Modify: `/home/shan/dev/herrscher-obsidian-memory/memory_test.go`

**Interfaces:**
- Consumes: `contracts.MetaLastSeen` (Task 1).
- Produces: every recorded node carries `Meta[contracts.MetaLastSeen]` (RFC3339 UTC); a caller-supplied value is honored verbatim; unlike `capturedAt`, an ordinary re-record **bumps** it to now.

- [ ] **Step 1: Write the failing test**

Add to `/home/shan/dev/herrscher-obsidian-memory/memory_test.go` (the package already exercises `ObsidianMemory` with an injectable `now`; follow the existing test's construction pattern — `New(t.TempDir())` then set `m.now`):

```go
func TestWriteNodeStampsAndBumpsLastSeen(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return t0 }

	ctx := context.Background()
	if err := m.Record(ctx, contracts.Node{Key: "n1", Body: "hello"}); err != nil {
		t.Fatal(err)
	}
	got, err := m.Recall(ctx, "n1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root.Meta[contracts.MetaLastSeen] != t0.Format(time.RFC3339) {
		t.Fatalf("lastSeen = %q, want %q", got.Root.Meta[contracts.MetaLastSeen], t0.Format(time.RFC3339))
	}
	capturedAt := got.Root.Meta["capturedAt"]

	// Re-record at a later clock with no lastSeen supplied: lastSeen bumps,
	// capturedAt is preserved.
	t1 := t0.Add(48 * time.Hour)
	m.now = func() time.Time { return t1 }
	if err := m.Record(ctx, contracts.Node{Key: "n1", Body: "hello again"}); err != nil {
		t.Fatal(err)
	}
	got, _ = m.Recall(ctx, "n1", 0)
	if got.Root.Meta[contracts.MetaLastSeen] != t1.Format(time.RFC3339) {
		t.Fatalf("lastSeen not bumped: %q", got.Root.Meta[contracts.MetaLastSeen])
	}
	if got.Root.Meta["capturedAt"] != capturedAt {
		t.Fatalf("capturedAt changed: %q want %q", got.Root.Meta["capturedAt"], capturedAt)
	}
}

func TestWriteNodeHonorsSuppliedLastSeen(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) }
	supplied := "2020-01-02T03:04:05Z"
	if err := m.Record(context.Background(), contracts.Node{
		Key: "n2", Body: "x", Meta: map[string]string{contracts.MetaLastSeen: supplied},
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Recall(context.Background(), "n2", 0)
	if got.Root.Meta[contracts.MetaLastSeen] != supplied {
		t.Fatalf("supplied lastSeen not honored: %q", got.Root.Meta[contracts.MetaLastSeen])
	}
}
```

(Ensure `context` and `time` are imported in the test file — they already are for the existing suite.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run 'TestWriteNode'`
Expected: FAIL (lastSeen empty).

- [ ] **Step 3: Write the implementation**

In `/home/shan/dev/herrscher-obsidian-memory/memory.go`, inside `writeNode`, immediately after the `capturedAt` stamping block (after `n.Meta["capturedAt"] = at` and its closing `}`, before `rel := keyToRel(n.Key)`):

```go
	// Stamp lastSeen (RFC3339 UTC) — the staleness machine's age basis. Unlike
	// capturedAt, it is NOT preserved on upsert: an ordinary re-record bumps it
	// to now (reactivation), while the curator sweep re-supplies the existing
	// value so a state-only write leaves the node's age untouched.
	if n.Meta[contracts.MetaLastSeen] == "" {
		if n.Meta == nil {
			n.Meta = map[string]string{}
		}
		n.Meta[contracts.MetaLastSeen] = m.now().UTC().Format(time.RFC3339)
	}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./...`
Expected: PASS (all).

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add memory.go memory_test.go
git commit -m "feat(memory): stamp lastSeen on every write, bumped on re-record (G3)"
```

---

## Task 3: obsidian — exclude archived from Search + Recall

**Files:**
- Modify: `/home/shan/dev/herrscher-obsidian-memory/memory.go` (`matchesQuery` ~line 339; `Recall` ~line 234)
- Modify: `/home/shan/dev/herrscher-obsidian-memory/memory_test.go`

**Interfaces:**
- Consumes: `contracts.MetaState`, `contracts.StateArchived`, `contracts.Query.IncludeArchived` (Task 1).
- Produces: `Search` hides archived nodes unless `Query.IncludeArchived`; `Recall` hides archived *neighbors* but always returns an explicitly-requested archived root.

- [ ] **Step 1: Write the failing test**

Add to `/home/shan/dev/herrscher-obsidian-memory/memory_test.go`:

```go
func TestArchivedExclusion(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	archived := map[string]string{contracts.MetaState: contracts.StateArchived}

	if err := m.Record(ctx, contracts.Node{Key: "root", Body: "keyword root",
		Links: []contracts.Link{{To: "old", Rel: "rel"}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Record(ctx, contracts.Node{Key: "old", Body: "keyword old", Meta: archived}); err != nil {
		t.Fatal(err)
	}

	// Search hides archived by default...
	hits, err := m.Search(ctx, contracts.Query{Text: "keyword"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range hits {
		if n.Key == "old" {
			t.Fatalf("archived node returned by default Search")
		}
	}
	// ...but IncludeArchived reaches it.
	hits, _ = m.Search(ctx, contracts.Query{Text: "keyword", IncludeArchived: true})
	found := false
	for _, n := range hits {
		if n.Key == "old" {
			found = true
		}
	}
	if !found {
		t.Fatalf("archived node missing with IncludeArchived=true")
	}

	// Recall from active root: archived neighbor is hidden.
	sg, err := m.Recall(ctx, "root", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range sg.Nodes {
		if n.Key == "old" {
			t.Fatalf("archived neighbor leaked into Recall")
		}
	}

	// Recall of an archived key directly: root still returned.
	sg, err = m.Recall(ctx, "old", 0)
	if err != nil {
		t.Fatal(err)
	}
	if sg.Root.Key != "old" {
		t.Fatalf("archived root not returned by direct Recall: %q", sg.Root.Key)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run TestArchivedExclusion`
Expected: FAIL (archived node leaks into Search/Recall).

- [ ] **Step 3: Implement Search exclusion**

In `matchesQuery` (`memory.go` ~line 339), add as the FIRST check, before the `Kinds` block:

```go
	if n.Meta[contracts.MetaState] == contracts.StateArchived && !q.IncludeArchived {
		return false
	}
```

- [ ] **Step 4: Implement Recall exclusion**

In `Recall` (`memory.go` ~line 234), immediately after the successful `child, err := m.loadUnlocked(l.To)` load (i.e. right after the `if err != nil { continue }` dangling-link guard, before `sg.Nodes = append(sg.Nodes, child)`):

```go
			if child.Meta[contracts.MetaState] == contracts.StateArchived {
				continue // archived neighbor: hide from graph expansion (root is always returned)
			}
```

The explicitly-requested root is loaded separately (`root, err := m.loadUnlocked(key)`) and is never subject to this check, so a direct `Recall` of an archived key still returns it.

- [ ] **Step 5: Run tests to verify pass**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./...`
Expected: PASS (all).

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add memory.go memory_test.go
git commit -m "feat(memory): hide archived nodes from Search/Recall, keep direct root (G3)"
```

---

## Task 4: orchestrator — Curator clock, windows, defaults, `SetStaleness`

**Files:**
- Modify: `/home/shan/dev/herrscher-orchestrator/orchestrator.go` (struct + `NewScoped` + new setter + `time` import)
- Test: covered by Task 5's `sweep_test.go` (the clock/windows have no behavior without `Sweep`); no standalone test in this task.

**Interfaces:**
- Produces:
  - `Curator` fields `now func() time.Time`, `staleAfter time.Duration`, `archiveAfter time.Duration`
  - defaults set in `NewScoped`: `now=time.Now`, `staleAfter=30d`, `archiveAfter=90d`
  - `func (c *Curator) SetStaleness(staleAfter, archiveAfter time.Duration)`

- [ ] **Step 1: Add the fields**

In `orchestrator.go`, add to the `Curator` struct (after `pending`):

```go
	now          func() time.Time      // injectable clock (defaults to time.Now); tests override
	staleAfter   time.Duration         // age before a node is marked stale (<=0 disables)
	archiveAfter time.Duration         // age before a node is archived (<=0 disables)
```

- [ ] **Step 2: Set defaults in `NewScoped`**

Replace the body of `NewScoped`:

```go
func NewScoped(mem contracts.Memory, session string, scope contracts.MemoryScope) *Curator {
	return &Curator{
		mem:          mem,
		session:      "sessions/" + session,
		scope:        scope,
		now:          time.Now,
		staleAfter:   30 * 24 * time.Hour,
		archiveAfter: 90 * 24 * time.Hour,
	}
}
```

- [ ] **Step 3: Add the setter**

Add to `orchestrator.go`:

```go
// SetStaleness configures the age windows used by Sweep. A window <= 0 disables
// that transition (nodes never reach that state). The host wires this from
// AGENT_STALE_DAYS / AGENT_ARCHIVE_DAYS in register.go.
func (c *Curator) SetStaleness(staleAfter, archiveAfter time.Duration) {
	c.staleAfter = staleAfter
	c.archiveAfter = archiveAfter
}
```

- [ ] **Step 4: Add the `time` import**

In `orchestrator.go`'s import block, add `"time"` (current imports: `context`, `fmt`, `strings`, contracts).

- [ ] **Step 5: Verify it compiles**

Run: `cd /home/shan/dev/herrscher-orchestrator && go build ./...`
Expected: builds clean.

- [ ] **Step 6: Commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
git add orchestrator.go
git commit -m "feat(orchestrator): Curator injectable clock + staleness windows + SetStaleness (G3)"
```

---

## Task 5: orchestrator — `Sweep` + best-effort call from `Consolidate`

**Files:**
- Create: `/home/shan/dev/herrscher-orchestrator/sweep.go`
- Create: `/home/shan/dev/herrscher-orchestrator/sweep_test.go`
- Modify: `/home/shan/dev/herrscher-orchestrator/learner.go` (`Consolidate` — call `Sweep` before final `return nil`)

**Interfaces:**
- Consumes: `contracts.NextState`, `contracts.MetaState`, `contracts.MetaLastSeen`, `contracts.StateActive`, `contracts.Query{IncludeArchived:true}` (Tasks 1–3); `Curator.now/staleAfter/archiveAfter` (Task 4).
- Produces: `func (c *Curator) Sweep(ctx context.Context) error`; `Learner.Consolidate` runs it best-effort (embedded `*Curator`, so `l.Sweep`).

- [ ] **Step 1: Write the failing test**

Create `/home/shan/dev/herrscher-orchestrator/sweep_test.go`. Use a small in-memory fake `contracts.Memory` that counts writes (the repo's existing tests already define fakes — follow that style; a self-contained fake is given here):

```go
package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts"
)

type fakeMem struct {
	nodes  map[string]contracts.Node
	writes int
}

func newFakeMem() *fakeMem { return &fakeMem{nodes: map[string]contracts.Node{}} }

func (f *fakeMem) Recall(ctx context.Context, key string, depth int) (contracts.Subgraph, error) {
	return contracts.Subgraph{Root: f.nodes[key]}, nil
}
func (f *fakeMem) Record(ctx context.Context, n contracts.Node) error {
	f.writes++
	f.nodes[n.Key] = n
	return nil
}
func (f *fakeMem) Search(ctx context.Context, q contracts.Query) ([]contracts.Node, error) {
	var out []contracts.Node
	for _, n := range f.nodes {
		if n.Meta[contracts.MetaState] == contracts.StateArchived && !q.IncludeArchived {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}
func (f *fakeMem) Links(ctx context.Context, from, to, rel string) error { return nil }

func seed(f *fakeMem, key string, ageDays int, now time.Time) {
	f.nodes[key] = contracts.Node{Key: key, Meta: map[string]string{
		contracts.MetaLastSeen: now.Add(-time.Duration(ageDays) * 24 * time.Hour).Format(time.RFC3339),
	}}
}

func TestSweepTransitions(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	f := newFakeMem()
	seed(f, "fresh", 1, now)
	seed(f, "old", 45, now)
	seed(f, "ancient", 200, now)

	c := NewScoped(f, "s", contracts.MemoryScope{})
	c.now = func() time.Time { return now }
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"fresh": contracts.StateActive, "old": contracts.StateStale, "ancient": contracts.StateArchived}
	for k, w := range want {
		if got := f.nodes[k].Meta[contracts.MetaState]; got != w {
			t.Fatalf("%s state = %q, want %q", k, got, w)
		}
	}
}

func TestSweepNoChurn(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	f := newFakeMem()
	seed(f, "fresh", 1, now) // already active (absent state == active)
	c := NewScoped(f, "s", contracts.MemoryScope{})
	c.now = func() time.Time { return now }
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.writes != 0 {
		t.Fatalf("unchanged node rewritten: writes=%d", f.writes)
	}
}

func TestSweepReactivation(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	f := newFakeMem()
	// A node that was archived but re-observed (fresh lastSeen) returns to active.
	f.nodes["r"] = contracts.Node{Key: "r", Meta: map[string]string{
		contracts.MetaState:    contracts.StateArchived,
		contracts.MetaLastSeen: now.Add(-time.Hour).Format(time.RFC3339),
	}}
	c := NewScoped(f, "s", contracts.MemoryScope{})
	c.now = func() time.Time { return now }
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.nodes["r"].Meta[contracts.MetaState]; got != contracts.StateActive {
		t.Fatalf("reactivation failed: state = %q", got)
	}
	if got := f.nodes["r"].Meta[contracts.MetaLastSeen]; got != now.Add(-time.Hour).Format(time.RFC3339) {
		t.Fatalf("lastSeen disturbed by state write: %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestSweep`
Expected: FAIL (undefined: Sweep).

- [ ] **Step 3: Write `Sweep`**

Create `/home/shan/dev/herrscher-orchestrator/sweep.go`:

```go
package orchestrator

import (
	"context"
	"time"

	"github.com/Herrscherd/herrscher-contracts"
)

// Sweep re-derives every node's lifecycle state from its lastSeen timestamp and
// persists any change. It is deterministic (clock injected via Curator.now) and
// best-effort: callers on the turn path must not fail a turn if Sweep errors.
//
// It enumerates archived nodes too (Query.IncludeArchived) so a re-observed node
// can transition back out of archived. A node with neither a lastSeen nor a
// capturedAt stamp has no age basis and is skipped. Unchanged nodes are never
// rewritten (no churn). When a state change is written, the existing lastSeen is
// re-supplied so obsidian's per-write lastSeen stamp does not bump the node's age
// (which would spuriously reactivate it).
func (c *Curator) Sweep(ctx context.Context) error {
	if c.mem == nil {
		return nil
	}
	nodes, err := c.mem.Search(ctx, contracts.Query{IncludeArchived: true})
	if err != nil {
		return err
	}
	now := c.now().UTC()
	for _, n := range nodes {
		stamp := n.Meta[contracts.MetaLastSeen]
		if stamp == "" {
			stamp = n.Meta["capturedAt"]
		}
		if stamp == "" {
			continue // no age basis
		}
		lastSeen, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			continue // unparseable timestamp
		}
		next := contracts.NextState(lastSeen, now, c.staleAfter, c.archiveAfter)
		cur := n.Meta[contracts.MetaState]
		if cur == "" {
			cur = contracts.StateActive
		}
		if next == cur {
			continue // no change: don't rewrite
		}
		if n.Meta == nil {
			n.Meta = map[string]string{}
		}
		n.Meta[contracts.MetaState] = next
		// Re-supply lastSeen so the state-only write does not reset the age.
		if n.Meta[contracts.MetaLastSeen] == "" {
			n.Meta[contracts.MetaLastSeen] = stamp
		}
		if err := c.mem.Record(ctx, n); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run TestSweep`
Expected: PASS.

- [ ] **Step 5: Call `Sweep` best-effort from `Consolidate`**

In `learner.go`, in `Consolidate`, replace the final `return nil` (after the `for _, c := range cands` loop) with:

```go
	// Best-effort staleness sweep at the end of a consolidation pass. A sweep
	// error must never propagate out of Consolidate (invariant: learning never
	// breaks a turn).
	_ = l.Sweep(ctx)
	return nil
```

- [ ] **Step 6: Run the full suite**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./...`
Expected: PASS (all).

- [ ] **Step 7: Commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
git add sweep.go sweep_test.go learner.go
git commit -m "feat(orchestrator): Sweep re-derives node state; Consolidate runs it best-effort (G3)"
```

---

## Task 6: orchestrator — env config in `register.go`

**Files:**
- Modify: `/home/shan/dev/herrscher-orchestrator/register.go` (Manifest `Config` + factory `SetStaleness` + `staleDuration` helper + `time` import)
- Test: `/home/shan/dev/herrscher-orchestrator/register_test.go` (create if absent)

**Interfaces:**
- Consumes: `Curator.SetStaleness` (Task 4), `contracts.Setting`, `contracts.Resolve` (existing).
- Produces: env `AGENT_STALE_DAYS` / `AGENT_ARCHIVE_DAYS` (integer days) → `SetStaleness` on both the `NewLearner` and `NewScoped` factory paths; unset → 30/90 defaults; `<= 0` disables.

> **Wiring note (verified 2026-07-30):** the host builds the local orchestrator through this plugin factory at `core/host/seed.go:298`, where `contracts.Resolve(plugin.Manifest.Config, os.Getenv)` resolves each declared `Setting.Env` into the `cfg` the factory reads. So this task is the complete env wiring — the host needs no code change (only the go.mod bump in Release Wiring). This mirrors obsidian's `OBSIDIAN_NODE_BUDGET`.

- [ ] **Step 1: Write the failing test**

Create/append `/home/shan/dev/herrscher-orchestrator/register_test.go`:

```go
package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestStaleDuration(t *testing.T) {
	cases := []struct {
		in       string
		def      int
		wantDays float64
	}{
		{"", 30, 30},
		{"45", 30, 45},
		{"garbage", 30, 30},
		{"0", 30, 0},
		{"-5", 30, -5},
	}
	for _, c := range cases {
		got := staleDuration(c.in, c.def)
		if got != time.Duration(c.wantDays*24)*time.Hour {
			t.Fatalf("staleDuration(%q,%d) = %v, want %v days", c.in, c.def, got, c.wantDays)
		}
	}
}

func TestOrchestratorFactoryWiresStaleness(t *testing.T) {
	var plugin contracts.Plugin
	for _, p := range contracts.Default.Orchestrators() {
		if p.Orchestrator != nil {
			plugin = p
			break
		}
	}
	if plugin.Orchestrator == nil {
		t.Fatal("no orchestrator plugin registered")
	}
	cfg := contracts.PluginConfig{Settings: map[string]string{
		"session":      "s",
		"stale-days":   "10",
		"archive-days": "20",
	}}
	o, err := plugin.Orchestrator(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := o.(*Curator)
	if !ok {
		t.Fatalf("want *Curator, got %T", o)
	}
	if c.staleAfter != 10*24*time.Hour || c.archiveAfter != 20*24*time.Hour {
		t.Fatalf("staleness not wired: stale=%v archive=%v", c.staleAfter, c.archiveAfter)
	}
}
```

(If `contracts.PluginConfig`'s accessor differs, match the existing shape — `cfg.Get(k)` reads `cfg.Settings[k]`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./... -run 'TestStaleDuration|TestOrchestratorFactory'`
Expected: FAIL (undefined: staleDuration; staleness not wired).

- [ ] **Step 3: Add the Manifest `Config`**

In `register.go`, add a `Config` to the `Manifest` (alongside `Kind`/`Category`):

```go
			Manifest: contracts.Manifest{
				Kind:     "basic",
				Category: contracts.CategoryOrchestrator,
				Config: []contracts.Setting{
					{Key: "stale-days", Env: "AGENT_STALE_DAYS", Help: "days of no re-observation before a node is marked stale; <=0 disables (default 30)", Required: false},
					{Key: "archive-days", Env: "AGENT_ARCHIVE_DAYS", Help: "days of no re-observation before a node is archived; <=0 disables (default 90)", Required: false},
				},
			},
```

- [ ] **Step 4: Wire `SetStaleness` in the factory + add the helper**

Replace the body of the `Orchestrator` factory func so both paths configure staleness:

```go
			Orchestrator: func(ctx context.Context, cfg contracts.PluginConfig, mem contracts.Memory) (contracts.Orchestrator, error) {
				var scope contracts.MemoryScope
				if p := cfg.Get("memory.project"); p != "" {
					scope.Project = contracts.ProjectKey(p)
				}
				if a := cfg.Get("memory.agent"); a != "" {
					scope.Agent = contracts.AgentKey(a)
				}
				stale := staleDuration(cfg.Get("stale-days"), 30)
				archive := staleDuration(cfg.Get("archive-days"), 90)
				if ex := lookupExtractor(cfg.Get("memory.extractor")); ex != nil {
					every, _ := strconv.Atoi(cfg.Get("memory.consolidate-every"))
					l := NewLearner(mem, cfg.Get("session"), scope, ex, cfg.Get("memory.journal"), every)
					l.SetStaleness(stale, archive)
					return l, nil
				}
				c := NewScoped(mem, cfg.Get("session"), scope)
				c.SetStaleness(stale, archive)
				return c, nil
			},
```

Add the helper at package scope in `register.go`:

```go
// staleDuration parses an integer-days config value into a Duration. Empty or
// unparseable → the default days. A value <= 0 is preserved (NextState treats
// it as "disable this transition").
func staleDuration(v string, defaultDays int) time.Duration {
	days := defaultDays
	if v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			days = n
		}
	}
	return time.Duration(days) * 24 * time.Hour
}
```

Add `"time"` to `register.go`'s imports (current: `context`, `strconv`, contracts).

- [ ] **Step 5: Run tests to verify pass**

Run: `cd /home/shan/dev/herrscher-orchestrator && go test ./...`
Expected: PASS (all).

- [ ] **Step 6: Verify the whole overlay + host still build/test**

Run: `cd <host> && go build ./... && go test ./core/... 2>&1 | tail -20`
Expected: builds clean; host tests pass against the overlay.

- [ ] **Step 7: Commit**

```bash
cd /home/shan/dev/herrscher-orchestrator
git add register.go register_test.go
git commit -m "feat(orchestrator): wire AGENT_STALE_DAYS/AGENT_ARCHIVE_DAYS via plugin config (G3)"
```

---

## Release Wiring (MAIN-AGENT ONLY — do NOT delegate to subagents)

All tag/push/network/`go get`/`go mod tidy` steps below are performed by the main agent, in dependency order. Subagents never run these.

- [ ] **Step 1: Tag + push contracts v0.2.9**

```bash
cd /home/shan/dev/herrscher-contracts
git push origin HEAD:master        # fast-forward master to the G3 commit
git tag v0.2.9 && git push origin v0.2.9
```

- [ ] **Step 2: Bump contracts in obsidian + orchestrator, then tag them**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
go get github.com/Herrscherd/herrscher-contracts@v0.2.9
go mod tidy && go build ./... && go test ./...
git add go.mod go.sum && git commit -m "chore: contracts v0.2.9 (G3)"
git push origin HEAD:master
git tag v0.2.7 && git push origin v0.2.7

cd /home/shan/dev/herrscher-orchestrator
go get github.com/Herrscherd/herrscher-contracts@v0.2.9
go mod tidy && go build ./... && go test ./...
git add go.mod go.sum && git commit -m "chore: contracts v0.2.9 (G3)"
git push origin HEAD:master
git tag v0.1.9 && git push origin v0.1.9
```

- [ ] **Step 3: Bump the host go.mod against the real tags**

```bash
cd <host>
rm go.work                          # release verification against real tags
go get github.com/Herrscherd/herrscher-contracts@v0.2.9
go get github.com/Herrscherd/herrscher-obsidian-memory@v0.2.7
go get github.com/Herrscherd/herrscher-orchestrator@v0.1.9
go mod tidy
GOWORK=off go build ./...
GOWORK=off go test ./...
```

Expected: contracts v0.2.9, obsidian v0.2.7, orchestrator v0.1.9 in `go.mod`; full suite green.

- [ ] **Step 4: Commit the host bump**

```bash
cd <host>
git add go.mod go.sum
git commit -m "chore(host): contracts v0.2.9 + obsidian v0.2.7 + orchestrator v0.1.9 (G3 staleness)"
```

---

## PR Finalization (MANDATORY — user standing instruction)

After the branch is implemented and released, run the full PR-finalization review before landing (see the saved `pr-finalization-checklist` memory): CI compliance, architecture decision, performance, code quality, security, bug review, strip useless comments, and update docs to match the actual project state (avoid false positives). Then use `superpowers:finishing-a-development-branch` to land.

---

## Self-Review (completed inline during authoring)

- **Spec coverage:** §1 contracts → Task 1; §2 obsidian lastSeen → Task 2, archived exclusion → Task 3; §3 orchestrator clock/windows → Task 4, Sweep + Consolidate → Task 5; §4 env config → Task 6 (corrected to `register.go`, matching the verified `seed.go:298` build seam); Release footprint → Release Wiring. All covered.
- **Placeholders:** none — every code step carries complete code.
- **Type consistency:** `NextState`, `MetaState`, `MetaLastSeen`, `StateActive/Stale/Archived`, `Query.IncludeArchived`, `SetStaleness`, `Sweep`, `staleDuration` used identically across tasks.
- **Invariants:** Sweep best-effort (Task 5 Step 5), label-only (no move/delete anywhere), pure transition in contracts (Task 1).
