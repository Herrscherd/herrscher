# G1 — Memory Node Budget + Forced Consolidation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `Memory.Record` refuse a node whose body exceeds a configured per-node budget, returning a typed "consolidate" error instead of silently appending — forcing atomicity at write time.

**Architecture:** A new typed error `contracts.BudgetError` in `herrscher-contracts`; the obsidian memory plugin gains a per-node rune budget (config `node-budget`, default 2000, 0 = disabled) enforced at the single write chokepoint `recordUnlocked` before `writeNode`. No new port methods; the check is policy inside the existing `Record` verb.

**Tech Stack:** Go, two modules — `github.com/Herrscherd/herrscher-contracts` and `github.com/Herrscherd/herrscher-obsidian-memory`.

## Global Constraints

- Ports-only: no new method on the `contracts.Memory` interface; enforcement lives inside the obsidian impl's existing `Record`/`recordUnlocked`.
- Budget is measured in **runes** (`utf8.RuneCountInString`) of `Node.Body`, not bytes.
- Budget `0` disables the check entirely (backward-compatible default for every existing caller/test that never sets it). The plugin default via config is `2000`.
- Learning never breaks a turn: the error is returned to the caller (Learner/CLI), which decides to consolidate; nothing panics or blocks.
- Cross-module dev: obsidian consumes the new `contracts.BudgetError`. During development both modules are wired via a local `go.work` (Task 0). Tagging contracts + bumping obsidian's `go.mod` is a release step, done by the main agent, out of scope for this plan's tests.

## File Structure

- `herrscher-contracts/budget.go` (create) — `BudgetError` typed error.
- `herrscher-contracts/budget_test.go` (create) — message + `errors.As` test.
- `herrscher-obsidian-memory/memory.go` (modify) — struct field `budget`, `SetNodeBudget`, enforcement in `recordUnlocked`.
- `herrscher-obsidian-memory/register.go` (modify) — `node-budget` setting + wiring.
- `herrscher-obsidian-memory/budget_test.go` (create) — over/under/disabled tests.
- `go.work` at `/home/shan/dev/` (create, dev-only) — spans both modules.

---

### Task 0: Local go.work so obsidian builds against local contracts

**Files:**
- Create: `/home/shan/dev/go.work`

**Interfaces:**
- Consumes: nothing.
- Produces: a workspace so `go test` in obsidian resolves `herrscher-contracts` to the local checkout (where Task 1 adds `BudgetError`).

- [ ] **Step 1: Create the workspace file**

```
go 1.23

use (
	./herrscher-contracts
	./herrscher-obsidian-memory
)
```

Write it to `/home/shan/dev/go.work`. (Match the `go` line to `go version`; bump if the toolchain is newer.)

- [ ] **Step 2: Verify both modules resolve**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go build ./...`
Expected: builds with no error (still green — no code changed yet).

- [ ] **Step 3: Commit is not needed** — `go.work` is dev-only and typically gitignored. Do NOT commit it. Confirm: `cd /home/shan/dev/herrscher-obsidian-memory && git status --short` shows no `go.work`.

---

### Task 1: `contracts.BudgetError` typed error

**Files:**
- Create: `/home/shan/dev/herrscher-contracts/budget.go`
- Test: `/home/shan/dev/herrscher-contracts/budget_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `type BudgetError struct { Key string; Runes int; Limit int }` with `func (*BudgetError) Error() string`. Obsidian (Task 2) returns `&contracts.BudgetError{...}` and callers match it with `errors.As(err, &*BudgetError)`.

- [ ] **Step 1: Write the failing test**

```go
package contracts_test

import (
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestBudgetErrorMessageAndAs(t *testing.T) {
	err := error(&contracts.BudgetError{Key: "projects/x/fact", Runes: 2100, Limit: 2000})

	msg := err.Error()
	for _, want := range []string{"projects/x/fact", "2100", "2000"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q missing %q", msg, want)
		}
	}

	var be *contracts.BudgetError
	if !errors.As(err, &be) {
		t.Fatal("errors.As failed to match *BudgetError")
	}
	if be.Runes != 2100 || be.Limit != 2000 {
		t.Fatalf("unexpected fields: %+v", be)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run TestBudgetErrorMessageAndAs -v`
Expected: FAIL — `undefined: contracts.BudgetError`.

- [ ] **Step 3: Write minimal implementation**

```go
package contracts

import "fmt"

// BudgetError is returned by Memory.Record when a node's Body exceeds the
// configured per-node budget. It carries the sizes so the caller (a Learner or
// a CLI verb) knows how much to trim, then consolidates/replaces to fit rather
// than blindly appending — the write-time atomicity forcer (memory roadmap G1).
// A budget of 0 disables the check, so this is never returned in that case.
type BudgetError struct {
	Key   string // node Key that was rejected
	Runes int    // rune count of the offered Body
	Limit int    // configured per-node budget, in runes
}

func (e *BudgetError) Error() string {
	return fmt.Sprintf(
		"memory: node %q body is %d runes, over the %d-rune budget; consolidate before recording",
		e.Key, e.Runes, e.Limit,
	)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-contracts && go test ./... -run TestBudgetErrorMessageAndAs -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/shan/dev/herrscher-contracts
git add budget.go budget_test.go
git commit -m "feat(memory): add BudgetError typed error for G1 node budget"
```

---

### Task 2: Enforce per-node budget in the obsidian plugin

**Files:**
- Modify: `/home/shan/dev/herrscher-obsidian-memory/memory.go` (struct near line 36; `recordUnlocked` at line 114)
- Modify: `/home/shan/dev/herrscher-obsidian-memory/register.go`
- Test: `/home/shan/dev/herrscher-obsidian-memory/budget_test.go` (create)

**Interfaces:**
- Consumes: `contracts.BudgetError` (Task 1).
- Produces: method `func (m *ObsidianMemory) SetNodeBudget(runes int)`; `Record` returns `*contracts.BudgetError` when `utf8.RuneCountInString(n.Body) > budget` and `budget > 0`.

- [ ] **Step 1: Write the failing test**

```go
package obsidian_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	obsidian "github.com/Herrscherd/herrscher-obsidian-memory"
)

func node(key, body string) contracts.Node {
	return contracts.Node{Key: key, Kind: contracts.KindDecision, Title: "T", Body: body}
}

func TestRecordRejectsOverBudget(t *testing.T) {
	m, err := obsidian.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.SetNodeBudget(50)

	// 100 accented runes = 200 bytes: proves the check counts runes, not bytes.
	err = m.Record(context.Background(), node("projects/x/fact", strings.Repeat("é", 100)))
	var be *contracts.BudgetError
	if !errors.As(err, &be) {
		t.Fatalf("want *BudgetError, got %v", err)
	}
	if be.Runes != 100 || be.Limit != 50 {
		t.Fatalf("unexpected sizes: %+v", be)
	}
}

func TestRecordAllowsUnderBudget(t *testing.T) {
	m, err := obsidian.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.SetNodeBudget(50)
	if err := m.Record(context.Background(), node("projects/x/fact", "short body")); err != nil {
		t.Fatalf("under budget should record, got %v", err)
	}
}

func TestZeroBudgetDisablesCheck(t *testing.T) {
	m, err := obsidian.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	// default budget is 0 (New does not set one) → no enforcement.
	if err := m.Record(context.Background(), node("projects/x/fact", strings.Repeat("x", 10_000))); err != nil {
		t.Fatalf("zero budget must not enforce, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run 'TestRecord|TestZeroBudget' -v`
Expected: FAIL — `m.SetNodeBudget undefined`.

- [ ] **Step 3: Add the struct field**

In `memory.go`, add to the `ObsidianMemory` struct (after `parseCache`):

```go
	// budget is the per-node Body rune budget enforced by Record; 0 disables it.
	// Guarded by mu (SetNodeBudget writes it, recordUnlocked reads it under mu).
	budget int
```

- [ ] **Step 4: Add the setter and the enforcement**

In `memory.go`, add the `unicode/utf8` import, then add the method:

```go
// SetNodeBudget sets the per-node Body budget in runes; 0 (the default) disables
// enforcement. When positive, Record returns *contracts.BudgetError for any node
// whose Body exceeds it — the caller must consolidate/replace to fit (G1).
func (m *ObsidianMemory) SetNodeBudget(runes int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.budget = runes
}
```

Replace `recordUnlocked` (currently `return m.writeNode(n, true)`) with:

```go
func (m *ObsidianMemory) recordUnlocked(n contracts.Node) error {
	if m.budget > 0 {
		if r := utf8.RuneCountInString(n.Body); r > m.budget {
			return &contracts.BudgetError{Key: n.Key, Runes: r, Limit: m.budget}
		}
	}
	return m.writeNode(n, true)
}
```

(`recordUnlocked` already runs under `m.mu` — `Record` locks it before calling — so the `m.budget` read is safe.)

- [ ] **Step 5: Run test to verify it passes**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./... -run 'TestRecord|TestZeroBudget' -v`
Expected: PASS.

- [ ] **Step 6: Wire config default in register.go**

Add the `strconv` import. Add the setting to the manifest `Config` slice:

```go
				{Key: "node-budget", Env: "OBSIDIAN_NODE_BUDGET", Help: "per-node Body budget in runes; 0 disables (default 2000)", Required: false},
```

In the `Memory:` builder, replace `return EnsureVault(root)` with:

```go
			mem, err := EnsureVault(root)
			if err != nil {
				return nil, err
			}
			budget := 2000
			if v := cfg.Get("node-budget"); v != "" {
				n, err := strconv.Atoi(v)
				if err != nil {
					return nil, fmt.Errorf("obsidian: node-budget: %w", err)
				}
				budget = n
			}
			mem.SetNodeBudget(budget)
			return mem, nil
```

- [ ] **Step 7: Run the full obsidian suite (no regressions)**

Run: `cd /home/shan/dev/herrscher-obsidian-memory && go test ./...`
Expected: all PASS (existing tests use `New`, which leaves budget 0 → unaffected).

- [ ] **Step 8: Commit**

```bash
cd /home/shan/dev/herrscher-obsidian-memory
git add memory.go register.go budget_test.go
git commit -m "feat(memory): enforce per-node rune budget in Record (G1)"
```

---

## Release note (out of plan scope, main agent)

Before the host consumes this: tag `herrscher-contracts` (minor bump for `BudgetError`), then `go get github.com/Herrscherd/herrscher-contracts@<tag>` + `go mod tidy` in obsidian, tag obsidian, then bump the host's `go.mod`. Remove the dev `go.work`. Per [[codex-exec-delegation]] the main agent owns these network/git steps.

## Self-Review

- **Spec coverage:** G1 = "budget + forced consolidation." Task 1 = typed error; Task 2 = enforcement + config default. The per-scope *aggregate* budget mentioned in the roadmap is deliberately deferred — per-node enforcement is the atomicity forcer that G1's rationale calls for, and is independently shippable; the aggregate becomes its own slice if needed (noted here, not silently dropped).
- **Placeholder scan:** none — every step has concrete code/commands.
- **Type consistency:** `BudgetError{Key, Runes, Limit}` used identically in Task 1 and Task 2; `SetNodeBudget(int)` matches between Interfaces and Step 4.
