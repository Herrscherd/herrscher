# G5 — Inactivity-triggered curator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a clock-driven inactivity trigger that fires the existing `Learner.Consolidate` out-of-band when a session has been quiet (`>= idle-days` since the last curator run AND `>= idle-hours` since the last observed turn), without ever blocking the turn path.

**Architecture:** A pure predicate `DueForIdleRun(now, lastActivity)` on `*Learner` plus a self-owned background poll goroutine (`Start`/`idleLoop`/`idleTick`), guarded by a `sync.Mutex` so the turn-path `Consolidate` and the idle-path `Consolidate` never interleave. The turn path uses a blocking `Lock`; the idle path uses non-blocking `TryLock` and skips a tick rather than wait. The host discovers `Start` as an OPTIONAL capability via a type assertion at one call site in `core/bridge/bridge.go` `Run`, exactly like `Provisioner`/`Locator`/`Deleter` — no `contracts.Orchestrator` method is added.

**Tech Stack:** Go, `github.com/Herrscherd/herrscher-orchestrator` (orchestrator plugin), `github.com/Herrscherd/herrscher` host (`core/bridge`), `github.com/Herrscherd/herrscher-contracts` (unchanged), `sync.Mutex`, `time.Ticker`, standard `testing`.

## Global Constraints

- **Scope:** `herrscher-orchestrator` (trigger policy, config, mutex, `Start`/`idleLoop`) + host (`core/bridge/bridge.go` one guarded `Start` call, `go.mod` bump, README). NO `herrscher-contracts` change, NO `herrscher-obsidian-memory` change.
- **Versions:** current published orchestrator tag is **v0.1.15**; G5 releases orchestrator **v0.1.16**; host bumps its orchestrator dep v0.1.15→v0.1.16. (Ignore any version number the spec's own text might imply.)
- **Invariant 1 — Ports only:** no `contracts.Orchestrator` method added; `Start` is an OPTIONAL capability discovered by type assertion `orch.(interface{ Start(context.Context) })` at the one host call site, same pattern as Provisioner/Locator/Deleter; `DueForIdleRun` is a pure function in orchestrator.
- **Invariant 2 — Learning never breaks a turn:** idle `Consolidate` runs from `idleLoop`'s own goroutine; the turn path must NEVER block waiting on the background curator.
- **Invariant 3 — Reversible:** G5 adds no write path, only a second TRIGGER for the existing reversible `Consolidate`.
- **Config:** two bare Settings — `idle-days`/`MEMORY_IDLE_DAYS` (default 0 = off, disables G5) and `idle-hours`/`MEMORY_IDLE_HOURS` (default 2), read in the Learner branch and pushed via `SetIdle(idleDays, idleHours)`. `SetIdle(sinceLastRunDays<=0)` disables.
- **`DueForIdleRun(now, lastActivity time.Time) bool` is PURE** — takes `now` as an explicit parameter (NOT via `l.now()` internally), returns false when disabled (`idleDays<=0`), when `lastRun` is zero (never consolidated — no baseline), or when either threshold unmet; thresholds are inclusive `>=` (Hermes semantics). Stamp `lastRun` at the top of `Consolidate`; stamp `lastActivity` at the top of `Observe`.
- **Concurrency (Go mutexes are NOT reentrant):** add `mu sync.Mutex` to `Learner`. Public `Consolidate(ctx)` = `Lock`/`defer Unlock` + call unexported `consolidateLocked(ctx)` (which holds the whole body incl. stamping `lastRun`). `Observe` takes the lock only briefly (stamp `lastActivity`, bump/read `n`, decide cadence), RELEASES it, then calls public `l.Consolidate(ctx)` only if due — never holding `mu` across that call. `idleLoop` evaluates the predicate under a brief `Lock`/`Unlock`, then uses `mu.TryLock()` before the idle `Consolidate`: on success it calls `consolidateLocked(ctx)` then `Unlock`; on `TryLock` failure it SKIPS the tick.
- **`var idlePollInterval = 10 * time.Minute`** is an unexported package-level `var` (NOT a `const`, NOT a Setting) so tests can shorten it to exercise `Start`/`idleLoop`+ctx-cancel without real waits. This is a deliberate, minimal deviation from the spec's word "constant", justified solely by `Start`/ctx-cancel testability.
- **Every `go` command uses `GOWORK=off`.**
- **Subagents commit LOCALLY only** (no push/tag/go-get). Task 6 is MAIN-AGENT-ONLY.

---

### Task 1: Fields + `SetIdle` + pure `DueForIdleRun`

**Files:**
- Modify: `/home/shan/dev/herrscher-orchestrator/learner.go` (struct fields on `Learner`, ~line 55-95; add `SetIdle` and `DueForIdleRun`)
- Test: `/home/shan/dev/herrscher-orchestrator/idle_test.go` (create)

**Interfaces:**
- Consumes: existing `Learner` struct and `NewLearner(mem contracts.Memory, session string, scope contracts.MemoryScope, ex Extractor, journal string, every int) *Learner` (learner.go); `l.now func() time.Time` (embedded `*Curator`, defaults to `time.Now`).
- Produces:
  - `func (l *Learner) SetIdle(sinceLastRunDays, idleHours int)` — sets `l.idleDays`, `l.idleHours`.
  - `func (l *Learner) DueForIdleRun(now, lastActivity time.Time) bool` — pure predicate.
  - new `Learner` fields: `idleDays int`, `idleHours int`, `lastRun time.Time`, `lastActivity time.Time`, `mu sync.Mutex`.

- [ ] **Step 1: Add `sync` import to learner.go**

`learner.go`'s import block currently is:

```go
import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"time"
	"unicode/utf8"

	"github.com/Herrscherd/herrscher-contracts"
)
```

Change it to add `sync`:

```go
import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Herrscherd/herrscher-contracts"
)
```

- [ ] **Step 2: Add the G5 fields to the `Learner` struct**

In `learner.go`, the `Learner` struct ends with the `reportEnabled`/`reportPrefix` fields just before the closing `}`:

```go
	// reportEnabled/reportPrefix configure the G4 audit-report pass
	// (Learner.report). Both are set via SetReport; register.go always calls
	// it (default enabled=true, prefix="reports/" — see the config table), so
	// an unconfigured host still gets a report.
	reportEnabled bool
	reportPrefix  string
}
```

Insert the G5 fields immediately before that closing `}`, so the block becomes:

```go
	// reportEnabled/reportPrefix configure the G4 audit-report pass
	// (Learner.report). Both are set via SetReport; register.go always calls
	// it (default enabled=true, prefix="reports/" — see the config table), so
	// an unconfigured host still gets a report.
	reportEnabled bool
	reportPrefix  string

	// idleDays/idleHours configure the G5 inactivity trigger (SetIdle).
	// idleDays <= 0 disables the trigger entirely (opt-in, default off).
	idleDays  int
	idleHours int
	// lastRun is stamped at the top of every Consolidate (zero = never run);
	// lastActivity is stamped at the top of every Observe. Both are guarded by
	// mu, which also serialises the whole Consolidate body so a turn-path call
	// and the idle-loop call never interleave (Go mutexes are not reentrant, so
	// the public Consolidate locks and delegates to consolidateLocked).
	lastRun      time.Time
	lastActivity time.Time
	mu           sync.Mutex
}
```

- [ ] **Step 3: Write the failing tests for `SetIdle` and `DueForIdleRun`**

Create `/home/shan/dev/herrscher-orchestrator/idle_test.go`:

```go
package orchestrator

import (
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts"
)

// idleLearner builds a Learner with the G5 idle trigger configured. A nil
// extractor keeps Consolidate a clean no-op except for stamping lastRun.
func idleLearner(days, hours int) *Learner {
	l := NewLearner(nil, "s", contracts.MemoryScope{}, nil, "", 0)
	l.SetIdle(days, hours)
	return l
}

func TestSetIdle(t *testing.T) {
	l := idleLearner(7, 3)
	if l.idleDays != 7 || l.idleHours != 3 {
		t.Fatalf("SetIdle did not apply: idleDays=%d idleHours=%d", l.idleDays, l.idleHours)
	}
	l.SetIdle(0, 2) // 0 days disables
	if l.idleDays != 0 {
		t.Fatalf("SetIdle(0,..) must leave idleDays=0 (disabled), got %d", l.idleDays)
	}
}

func TestDueForIdleRun(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour

	cases := []struct {
		name         string
		idleDays     int
		idleHours    int
		lastRun      time.Time
		lastActivity time.Time
		want         bool
	}{
		{
			name:     "disabled (idleDays<=0) always false",
			idleDays: 0, idleHours: 2,
			lastRun:      now.Add(-30 * day),
			lastActivity: now.Add(-30 * day),
			want:         false,
		},
		{
			name:     "lastRun zero (never consolidated) false",
			idleDays: 7, idleHours: 2,
			lastRun:      time.Time{},
			lastActivity: now.Add(-30 * day),
			want:         false,
		},
		{
			name:     "sinceLastRun < idleDays false even if activity ancient",
			idleDays: 7, idleHours: 2,
			lastRun:      now.Add(-3 * day),
			lastActivity: now.Add(-30 * day),
			want:         false,
		},
		{
			name:     "idle < idleHours (recent turn) false",
			idleDays: 7, idleHours: 2,
			lastRun:      now.Add(-10 * day),
			lastActivity: now.Add(-1 * time.Hour),
			want:         false,
		},
		{
			name:     "both thresholds met true",
			idleDays: 7, idleHours: 2,
			lastRun:      now.Add(-10 * day),
			lastActivity: now.Add(-5 * time.Hour),
			want:         true,
		},
		{
			name:     "boundary equality inclusive (>=) true",
			idleDays: 7, idleHours: 2,
			lastRun:      now.Add(-7 * day),
			lastActivity: now.Add(-2 * time.Hour),
			want:         true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := idleLearner(c.idleDays, c.idleHours)
			l.lastRun = c.lastRun
			if got := l.DueForIdleRun(now, c.lastActivity); got != c.want {
				t.Errorf("DueForIdleRun = %v, want %v", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `cd /home/shan/dev/herrscher-orchestrator && GOWORK=off go test ./... -run 'TestSetIdle|TestDueForIdleRun' -v`
Expected: FAIL — `l.SetIdle undefined` / `l.DueForIdleRun undefined` (compile error).

- [ ] **Step 5: Implement `SetIdle` and `DueForIdleRun`**

In `learner.go`, add these two methods (place them right after `NewLearner`, before `Observe`):

```go
// SetIdle configures the G5 inactivity trigger. sinceLastRunDays <= 0 disables
// it (opt-in, default off); idleHours is the quiet-period threshold measured
// against lastActivity. Mirrors SetStaleness/SetMerge's setter shape.
func (l *Learner) SetIdle(sinceLastRunDays, idleHours int) {
	l.idleDays = sinceLastRunDays
	l.idleHours = idleHours
}

// DueForIdleRun reports whether an inactivity-triggered Consolidate should fire,
// given the current time and the timestamp of the last observed activity. It is
// pure: it reads now as an explicit parameter (not l.now()), so it is unit-
// testable with fixed times and needs no real timers. Mirrors Hermes:
// (now-lastRun >= idleDays) AND (now-lastActivity >= idleHours), both inclusive.
// It returns false when the trigger is disabled (idleDays <= 0), when lastRun is
// zero (never consolidated: no baseline yet), or when either threshold is unmet.
func (l *Learner) DueForIdleRun(now, lastActivity time.Time) bool {
	if l.idleDays <= 0 {
		return false
	}
	if l.lastRun.IsZero() {
		return false
	}
	if now.Sub(l.lastRun) < time.Duration(l.idleDays)*24*time.Hour {
		return false
	}
	if now.Sub(lastActivity) < time.Duration(l.idleHours)*time.Hour {
		return false
	}
	return true
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd /home/shan/dev/herrscher-orchestrator && GOWORK=off go test ./... -run 'TestSetIdle|TestDueForIdleRun' -v`
Expected: PASS (all subtests).

- [ ] **Step 7: Commit (LOCAL only)**

```bash
cd /home/shan/dev/herrscher-orchestrator && git add learner.go idle_test.go && git commit -m "feat(memory): G5 fields + SetIdle + pure DueForIdleRun predicate"
```

---

### Task 2: Mutex refactor — split `Consolidate`→`consolidateLocked`, stamp `lastRun`; `Observe` stamps `lastActivity` off-lock

**Files:**
- Modify: `/home/shan/dev/herrscher-orchestrator/learner.go` (`Observe` ~line 107-116; `Consolidate` ~line 121-181)
- Test: `/home/shan/dev/herrscher-orchestrator/idle_test.go` (append)

**Interfaces:**
- Consumes: `Learner.mu`, `l.lastRun`, `l.lastActivity`, `l.now()`, `l.every`, `l.n` (Task 1 / existing); existing `Consolidate` body (extract/persist/sweep/merge/promote/report).
- Produces:
  - `func (l *Learner) Consolidate(ctx context.Context) error` — now a thin `Lock`/`defer Unlock` wrapper delegating to `consolidateLocked` (public signature unchanged; still safe to call standalone).
  - `func (l *Learner) consolidateLocked(ctx context.Context) error` — holds the whole existing body, stamps `l.lastRun = l.now()` at the top. **Caller must already hold `l.mu`.**
  - `Observe` now stamps `l.lastActivity` and reads/increments `l.n` under a brief lock, then calls `l.Consolidate(ctx)` OFF the lock.

- [ ] **Step 1: Write the failing tests (lastRun stamping, lastActivity stamping, race)**

Append to `/home/shan/dev/herrscher-orchestrator/idle_test.go`. First add the imports it needs — change the file's import block to:

```go
import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts"
)
```

Then append these tests:

```go
// fakeClock returns a time it can be advanced past for deterministic stamping.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time  { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }

func TestConsolidateStampsLastRun(t *testing.T) {
	// nil extractor + nil mem: Consolidate is a no-op except for stamping lastRun.
	l := NewLearner(nil, "s", contracts.MemoryScope{}, nil, "", 0)
	clk := &fakeClock{t: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)}
	l.now = clk.now

	if !l.lastRun.IsZero() {
		t.Fatal("lastRun should start zero")
	}
	if err := l.Consolidate(context.Background()); err != nil {
		t.Fatalf("Consolidate 1: %v", err)
	}
	first := l.lastRun
	if first != clk.t {
		t.Fatalf("lastRun not stamped on first Consolidate: got %v want %v", first, clk.t)
	}
	clk.add(3 * time.Hour) // a later manual/no-trigger call must re-stamp
	if err := l.Consolidate(context.Background()); err != nil {
		t.Fatalf("Consolidate 2: %v", err)
	}
	if !l.lastRun.After(first) {
		t.Fatalf("lastRun did not advance across a second Consolidate: %v then %v", first, l.lastRun)
	}
}

func TestObserveStampsLastActivity(t *testing.T) {
	// nil mem: Curator.Observe is a no-op, cadence disabled (every=0), so Observe
	// only stamps lastActivity — no Consolidate fires.
	l := NewLearner(nil, "s", contracts.MemoryScope{}, nil, "", 0)
	clk := &fakeClock{t: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)}
	l.now = clk.now

	if err := l.Observe(context.Background(), contracts.Prompt{Author: "a", Content: "hi"}, "yo"); err != nil {
		t.Fatalf("Observe 1: %v", err)
	}
	first := l.lastActivity
	if first != clk.t {
		t.Fatalf("lastActivity not stamped: got %v want %v", first, clk.t)
	}
	clk.add(time.Hour)
	if err := l.Observe(context.Background(), contracts.Prompt{Author: "a", Content: "hi"}, "yo"); err != nil {
		t.Fatalf("Observe 2: %v", err)
	}
	if !l.lastActivity.After(first) {
		t.Fatalf("lastActivity did not advance across a second Observe: %v then %v", first, l.lastActivity)
	}
}

// TestConcurrentObserveAndConsolidate exercises the interleaving of a turn-path
// Observe (which may fire the cadence Consolidate) and a forced idle-triggered
// Consolidate from a second goroutine. Its acceptance bar is a clean `-race`
// run: no data race, no panic. Uses a real (empty) mergeMem so the full
// Consolidate body (extract/sweep/merge/promote/report) executes.
func TestConcurrentObserveAndConsolidate(t *testing.T) {
	mem := &mergeMem{}
	l := NewLearner(mem, "s", contracts.MemoryScope{}, plainExt{}, "", 1) // every=1: Observe fires Consolidate each turn
	l.SetIdle(1, 1)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = l.Observe(context.Background(), contracts.Prompt{Author: "a", Content: "hi"}, "yo")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = l.Consolidate(context.Background()) // forced idle-style call
		}
	}()
	wg.Wait()
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /home/shan/dev/herrscher-orchestrator && GOWORK=off go test ./... -run 'TestConsolidateStampsLastRun|TestObserveStampsLastActivity' -v`
Expected: FAIL — `lastRun not stamped` / `lastActivity not stamped` (the current `Observe`/`Consolidate` do not stamp these fields yet).

- [ ] **Step 3: Refactor `Observe` to stamp `lastActivity` and call `Consolidate` off-lock**

In `learner.go`, replace the current `Observe`:

```go
// Observe records the turn (default behaviour) and, every `every` turns, fires a
// best-effort Consolidate out of band so learning never breaks the turn loop.
func (l *Learner) Observe(ctx context.Context, p contracts.Prompt, reply string) error {
	err := l.Curator.Observe(ctx, p, reply)
	if l.every > 0 {
		l.n++
		if l.n%l.every == 0 {
			_ = l.Consolidate(ctx)
		}
	}
	return err
}
```

with:

```go
// Observe records the turn (default behaviour), stamps the last-activity clock
// (the G5 idle trigger's activity signal), and, every `every` turns, fires a
// best-effort Consolidate out of band so learning never breaks the turn loop.
//
// mu is held only briefly — to stamp lastActivity and read/advance the turn
// counter — and is RELEASED before calling l.Consolidate, which re-acquires it.
// Holding mu across that call would double-lock (Go mutexes are not reentrant)
// and deadlock.
func (l *Learner) Observe(ctx context.Context, p contracts.Prompt, reply string) error {
	err := l.Curator.Observe(ctx, p, reply)
	l.mu.Lock()
	l.lastActivity = l.now()
	var due bool
	if l.every > 0 {
		l.n++
		due = l.n%l.every == 0
	}
	l.mu.Unlock()
	if due {
		_ = l.Consolidate(ctx)
	}
	return err
}
```

- [ ] **Step 4: Split `Consolidate` into a locking wrapper + `consolidateLocked`, stamp `lastRun`**

In `learner.go`, the current `Consolidate` begins:

```go
func (l *Learner) Consolidate(ctx context.Context) error {
	// Reset the pass-scoped audit trail on every return path, so the next pass
	// never re-reports this pass's transitions. A deferred reset also caps the
	// out-of-band buffer: Learner.Restore appends transitions between passes, so
	// a nil-extractor Learner (whose Consolidate returns early below) would
	// otherwise accumulate them forever. report() runs before this fires and so
	// still sees the full trail on the normal path.
	defer func() { l.transitions = nil }()
	if l.extract == nil || l.mem == nil {
		return nil
	}
```

Replace only the function signature line and insert the wrapper. Change the opening so it reads:

```go
// Consolidate runs the extractor over the journal + transcript and persists each
// candidate under the right scope. It is best-effort: a missing journal, a nil
// extractor, or a nil Memory all yield a clean no-op. It is safe to call from
// any goroutine (turn-path cadence, the G5 idle loop, or a standalone/test
// call): it serialises every pass under mu so two passes never interleave.
func (l *Learner) Consolidate(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.consolidateLocked(ctx)
}

// consolidateLocked holds the full consolidation body. The caller MUST already
// hold l.mu (public Consolidate acquires it; the idle loop acquires it via
// TryLock). It stamps lastRun at the top so every trigger path — per-turn
// cadence, idle, or a manual call — uniformly resets the inactivity clock.
func (l *Learner) consolidateLocked(ctx context.Context) error {
	l.lastRun = l.now()
	// Reset the pass-scoped audit trail on every return path, so the next pass
	// never re-reports this pass's transitions. A deferred reset also caps the
	// out-of-band buffer: Learner.Restore appends transitions between passes, so
	// a nil-extractor Learner (whose Consolidate returns early below) would
	// otherwise accumulate them forever. report() runs before this fires and so
	// still sees the full trail on the normal path.
	defer func() { l.transitions = nil }()
	if l.extract == nil || l.mem == nil {
		return nil
	}
```

Leave the rest of the original body (from `var firstErr error` through `return firstErr`) unchanged — it now belongs to `consolidateLocked`.

- [ ] **Step 5: Run the stamping tests to verify they pass**

Run: `cd /home/shan/dev/herrscher-orchestrator && GOWORK=off go test ./... -run 'TestConsolidateStampsLastRun|TestObserveStampsLastActivity' -v`
Expected: PASS.

- [ ] **Step 6: Run the whole package under `-race` (covers the concurrency test + no regressions)**

Run: `cd /home/shan/dev/herrscher-orchestrator && GOWORK=off go test -race ./...`
Expected: PASS (`ok`), no `DATA RACE` report. Confirms the mutex refactor did not break the existing merge/promote/report tests and that concurrent Observe + Consolidate are race-free.

- [ ] **Step 7: Commit (LOCAL only)**

```bash
cd /home/shan/dev/herrscher-orchestrator && git add learner.go idle_test.go && git commit -m "feat(memory): serialise Consolidate under mu; Observe stamps lastActivity off-lock"
```

---

### Task 3: `Start` + `idleLoop` + `idleTick` + `idlePollInterval` var

**Files:**
- Modify: `/home/shan/dev/herrscher-orchestrator/learner.go` (add `idlePollInterval` var + `Start`/`idleLoop`/`idleTick`)
- Test: `/home/shan/dev/herrscher-orchestrator/idle_test.go` (append)

**Interfaces:**
- Consumes: `Learner.mu`, `l.DueForIdleRun`, `l.now()`, `l.lastActivity`, `l.consolidateLocked(ctx)`, `l.idleDays` (Tasks 1-2).
- Produces:
  - `var idlePollInterval = 10 * time.Minute` — package-level, mutable for tests only.
  - `func (l *Learner) Start(ctx context.Context)` — spawns `idleLoop` in a goroutine; no-op when `idleDays <= 0`.
  - `func (l *Learner) idleLoop(ctx context.Context)` — ticker loop, calls `idleTick` per tick, returns on `ctx.Done()`.
  - `func (l *Learner) idleTick(ctx context.Context)` — one tick: evaluate predicate under brief lock, then `TryLock`-guarded `consolidateLocked`; skips on `TryLock` failure. Directly callable by tests with a fake clock.

- [ ] **Step 1: Write the failing tests for `idleTick` and `Start`**

Append to `/home/shan/dev/herrscher-orchestrator/idle_test.go`. `countExt` is an `Extractor` that counts how many times `Consolidate` reached the extract step (a Consolidate-run spy):

```go
// countExt is an Extractor that counts Extract calls — a proxy for "Consolidate
// actually ran its body". Consolidate only calls Extract when both extract and
// mem are non-nil, so pair it with a non-nil mergeMem.
type countExt struct {
	mu sync.Mutex
	n  int
}

func (c *countExt) Extract(context.Context, string, string) ([]Candidate, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return nil, nil
}
func (c *countExt) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// idleTickLearner builds a Learner whose Consolidate body runs (non-nil mem +
// countExt) with the idle trigger configured and a fixed clock.
func idleTickLearner(days, hours int, now time.Time) (*Learner, *countExt) {
	ext := &countExt{}
	l := NewLearner(&mergeMem{}, "s", contracts.MemoryScope{}, ext, "", 0)
	l.SetIdle(days, hours)
	l.now = func() time.Time { return now }
	return l, ext
}

func TestIdleTickFiresWhenDue(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	l, ext := idleTickLearner(7, 2, now)
	l.lastRun = now.Add(-10 * 24 * time.Hour)      // 10 days ago >= 7
	l.lastActivity = now.Add(-5 * time.Hour)        // 5h idle >= 2h
	l.idleTick(context.Background())
	if ext.count() != 1 {
		t.Fatalf("idleTick did not fire Consolidate once when due; extract calls=%d", ext.count())
	}
}

func TestIdleTickSkipsWhenNotDue(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	l, ext := idleTickLearner(7, 2, now)
	l.lastRun = now.Add(-10 * 24 * time.Hour)
	l.lastActivity = now.Add(-30 * time.Minute) // only 30m idle < 2h → not due
	l.idleTick(context.Background())
	if ext.count() != 0 {
		t.Fatalf("idleTick fired when not due; extract calls=%d", ext.count())
	}
}

func TestStartDisabledSpawnsNothing(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	l, ext := idleTickLearner(0, 2, now) // idleDays=0 → disabled
	l.lastRun = now.Add(-100 * 24 * time.Hour)
	l.lastActivity = now.Add(-100 * time.Hour)

	old := idlePollInterval
	idlePollInterval = time.Millisecond
	defer func() { idlePollInterval = old }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l.Start(ctx)
	time.Sleep(30 * time.Millisecond) // several would-be ticks
	if ext.count() != 0 {
		t.Fatalf("disabled Start must never fire Consolidate; extract calls=%d", ext.count())
	}
}

func TestStartFiresWhenDueThenStopsOnCancel(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	l, ext := idleTickLearner(7, 2, now)
	l.lastRun = now.Add(-10 * 24 * time.Hour)
	l.lastActivity = now.Add(-5 * time.Hour)

	old := idlePollInterval
	idlePollInterval = time.Millisecond
	defer func() { idlePollInterval = old }()

	ctx, cancel := context.WithCancel(context.Background())
	l.Start(ctx)

	// Wait (bounded) for at least one idle-triggered Consolidate.
	deadline := time.Now().Add(2 * time.Second)
	for ext.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if ext.count() == 0 {
		t.Fatal("Start never fired an idle Consolidate when due")
	}

	cancel()
	time.Sleep(20 * time.Millisecond) // let the loop observe ctx.Done and return
	stopped := ext.count()
	time.Sleep(30 * time.Millisecond)
	if ext.count() != stopped {
		t.Fatalf("idle loop kept firing after ctx cancel: %d -> %d", stopped, ext.count())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /home/shan/dev/herrscher-orchestrator && GOWORK=off go test ./... -run 'TestIdleTick|TestStart' -v`
Expected: FAIL — `l.idleTick undefined`, `l.Start undefined`, `idlePollInterval undefined` (compile error).

- [ ] **Step 3: Implement `idlePollInterval`, `Start`, `idleLoop`, `idleTick`**

In `learner.go`, add near the top of the file (right after the `errEnqueue` var declaration, before the `Learner` type):

```go
// idlePollInterval is how often the G5 idle loop checks DueForIdleRun. It is a
// package var (not a const) solely so tests can shorten it to exercise
// Start/idleLoop and ctx-cancellation without real waits; it only needs to be
// finer than idle-hours' granularity to detect the threshold promptly.
var idlePollInterval = 10 * time.Minute
```

Then add these three methods (place them after `DueForIdleRun`):

```go
// Start runs the G5 inactivity poll loop until ctx is cancelled. It is a no-op
// if the idle trigger is disabled (SetIdle never called or idleDays <= 0). The
// host calls this once per bridge process, right after constructing the
// orchestrator; it never blocks a turn — the loop only ever fires Consolidate
// from its own goroutine, single-flighted against the turn path via TryLock.
func (l *Learner) Start(ctx context.Context) {
	if l.idleDays <= 0 {
		return
	}
	go l.idleLoop(ctx)
}

// idleLoop ticks every idlePollInterval and evaluates one idleTick per tick,
// returning when ctx is cancelled (the bridge process's root context, so the
// goroutine is cleaned up automatically on session teardown).
func (l *Learner) idleLoop(ctx context.Context) {
	t := time.NewTicker(idlePollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.idleTick(ctx)
		}
	}
}

// idleTick evaluates the inactivity predicate and, if due, runs one Consolidate
// out of band. It first reads lastActivity/lastRun under a brief Lock (always
// safe to wait for). If due, it takes mu with TryLock — never Lock — so it never
// blocks the turn path (invariant 2): on success it runs consolidateLocked while
// holding the lock; on TryLock failure (a turn-path Consolidate holds the lock)
// it skips this tick and retries at the next one. Extracted from idleLoop so
// tests can drive it directly with a fake clock.
func (l *Learner) idleTick(ctx context.Context) {
	l.mu.Lock()
	due := l.DueForIdleRun(l.now(), l.lastActivity)
	l.mu.Unlock()
	if !due {
		return
	}
	if !l.mu.TryLock() {
		return // turn-path Consolidate holds the lock; never block — retry next tick
	}
	_ = l.consolidateLocked(ctx) // best-effort; error swallowed, like the cadence path
	l.mu.Unlock()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /home/shan/dev/herrscher-orchestrator && GOWORK=off go test ./... -run 'TestIdleTick|TestStart' -v`
Expected: PASS (all four tests).

- [ ] **Step 5: Run the full package under `-race`**

Run: `cd /home/shan/dev/herrscher-orchestrator && GOWORK=off go test -race ./...`
Expected: PASS, no `DATA RACE` (the `Start` loop goroutine + fake-clock reads are race-clean).

- [ ] **Step 6: Commit (LOCAL only)**

```bash
cd /home/shan/dev/herrscher-orchestrator && git add learner.go idle_test.go && git commit -m "feat(memory): G5 Start/idleLoop/idleTick with TryLock-guarded idle Consolidate"
```

---

### Task 4: `register.go` config triple `idle-days`/`idle-hours` + `SetIdle` wiring

**Files:**
- Modify: `/home/shan/dev/herrscher-orchestrator/register.go` (Config manifest list ~line 16-25; Learner branch ~line 45-56)
- Test: `/home/shan/dev/herrscher-orchestrator/register_test.go` (append)

**Interfaces:**
- Consumes: `contracts.Setting{Key, Env, Help, Required}`, `cfg.Get(key)`, `strconv.Atoi`, `l.SetIdle(sinceLastRunDays, idleHours int)` (Task 1), the plugin enumerator `contracts.Default.Orchestrators()` (confirmed used in `register_test.go`).
- Produces: two new manifest Settings (`idle-days`/`MEMORY_IDLE_DAYS`, `idle-hours`/`MEMORY_IDLE_HOURS`) and a `SetIdle` call in the Learner branch.

- [ ] **Step 1: Write the failing manifest test**

Append to `/home/shan/dev/herrscher-orchestrator/register_test.go`:

```go
func TestManifestHasIdleSettings(t *testing.T) {
	want := map[string]string{
		"idle-days":  "MEMORY_IDLE_DAYS",
		"idle-hours": "MEMORY_IDLE_HOURS",
	}
	found := map[string]string{}
	for _, p := range contracts.Default.Orchestrators() {
		if p.Manifest.Category != contracts.CategoryOrchestrator {
			continue
		}
		for i := range p.Manifest.Config {
			s := p.Manifest.Config[i]
			if _, ok := want[s.Key]; ok {
				found[s.Key] = s.Env
			}
		}
	}
	for key, env := range want {
		if found[key] != env {
			t.Errorf("manifest setting %q: env=%q, want %q", key, found[key], env)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /home/shan/dev/herrscher-orchestrator && GOWORK=off go test ./... -run TestManifestHasIdleSettings -v`
Expected: FAIL — `manifest setting "idle-days": env="", want "MEMORY_IDLE_DAYS"` (settings not registered yet).

- [ ] **Step 3: Add the two Settings to the manifest**

In `register.go`, the `Config` list currently ends with the `promote-min-age-days` entry:

```go
				{Key: "promote-min-age-days", Env: "MEMORY_PROMOTE_MIN_AGE_DAYS", Help: "days a private node's lastSeen must exceed its capturedAt before the curator promotes it to the shared project scope; <=0 disables (default 0, off)", Required: false},
			},
```

Insert the two G5 settings immediately after that entry, before the closing `},`:

```go
				{Key: "promote-min-age-days", Env: "MEMORY_PROMOTE_MIN_AGE_DAYS", Help: "days a private node's lastSeen must exceed its capturedAt before the curator promotes it to the shared project scope; <=0 disables (default 0, off)", Required: false},
				{Key: "idle-days", Env: "MEMORY_IDLE_DAYS", Help: "days since the last Consolidate run before the G5 inactivity trigger may fire; <=0 disables G5 (default 0, off)", Required: false},
				{Key: "idle-hours", Env: "MEMORY_IDLE_HOURS", Help: "hours of quiet (no observed turn) required, once idle-days has elapsed, before the idle trigger fires; only consulted when idle-days > 0 (default 2)", Required: false},
			},
```

- [ ] **Step 4: Wire `SetIdle` in the Learner branch**

In `register.go`, the Learner branch currently ends with the promote wiring then `return l, nil`:

```go
				promoteDays, _ := strconv.Atoi(cfg.Get("promote-min-age-days"))
				l.SetPromote(time.Duration(promoteDays) * 24 * time.Hour)
				return l, nil
```

Insert the idle wiring between `SetPromote` and `return l, nil`. Note the `idle-hours` default of 2 is applied only when the value is empty/unparseable (mirrors how `staleDuration` defaults), while `idle-days` defaults to 0 (off) via `Atoi`'s zero on empty:

```go
				promoteDays, _ := strconv.Atoi(cfg.Get("promote-min-age-days"))
				l.SetPromote(time.Duration(promoteDays) * 24 * time.Hour)
				idleDays, _ := strconv.Atoi(cfg.Get("idle-days"))
				idleHours := 2
				if v, err := strconv.Atoi(cfg.Get("idle-hours")); err == nil {
					idleHours = v
				}
				l.SetIdle(idleDays, idleHours)
				return l, nil
```

- [ ] **Step 5: Run the manifest test + full package**

Run: `cd /home/shan/dev/herrscher-orchestrator && GOWORK=off go test ./...`
Expected: PASS (`ok`), including `TestManifestHasIdleSettings`.

- [ ] **Step 6: Commit (LOCAL only)**

```bash
cd /home/shan/dev/herrscher-orchestrator && git add register.go register_test.go && git commit -m "feat(memory): register idle-days/idle-hours settings and wire SetIdle"
```

---

### Task 5: Host — guarded `Start` type-assertion call in `core/bridge/bridge.go` `Run`

**Files:**
- Modify: `/home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review/core/bridge/bridge.go` (`Run` ~line 37-42)
- Test: `/home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review/core/bridge/bridge_g5_test.go` (create)

**Interfaces:**
- Consumes: existing `func Run(ctx context.Context, newBackend BackendFactory, orch contracts.Orchestrator, o Options) error`; the optional-capability pattern `orch.(interface{ Start(context.Context) })`.
- Produces: a guarded `Start(ctx)` call at the top of `Run` (a plain orchestrator without `Start` is unaffected). `RunOneShot` is NOT touched.

- [ ] **Step 1: Write the failing test**

Create `/home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review/core/bridge/bridge_g5_test.go`:

```go
package bridge

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

// startOrch is a full contracts.Orchestrator that ALSO exposes the optional
// Start(context.Context) capability G5 discovers by type assertion.
type startOrch struct{ started atomic.Bool }

func (o *startOrch) Context(context.Context) string                            { return "" }
func (o *startOrch) Observe(context.Context, contracts.Prompt, string) error   { return nil }
func (o *startOrch) Consolidate(context.Context) error                         { return nil }
func (o *startOrch) Close() error                                              { return nil }
func (o *startOrch) Start(context.Context)                                     { o.started.Store(true) }

// plainOrch is a full contracts.Orchestrator WITHOUT Start.
type plainOrch struct{}

func (plainOrch) Context(context.Context) string                          { return "" }
func (plainOrch) Observe(context.Context, contracts.Prompt, string) error { return nil }
func (plainOrch) Consolidate(context.Context) error                       { return nil }
func (plainOrch) Close() error                                            { return nil }

// failBackend makes runHub return early (before any real I/O) so Run exercises
// only the Start dispatch, then unwinds. The Start call happens before runHub.
func failBackend(string) (contracts.Backend, error) {
	return nil, errors.New("no backend in test")
}

func TestRunStartsOptionalCapability(t *testing.T) {
	orch := &startOrch{}
	opts := Options{Channel: "c", HubSocket: "/nonexistent-hub.sock"}
	_ = Run(context.Background(), failBackend, orch, opts) // error expected from runHub
	if !orch.started.Load() {
		t.Fatal("Run did not invoke the orchestrator's optional Start capability")
	}
}

func TestRunWithoutStartCapabilityStillRuns(t *testing.T) {
	opts := Options{Channel: "c", HubSocket: "/nonexistent-hub.sock"}
	// Must not panic on the missing Start; the type assertion is guarded.
	if err := Run(context.Background(), failBackend, plainOrch{}, opts); err == nil {
		t.Fatal("expected Run to surface the backend error")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review && GOWORK=off go test ./core/bridge/ -run 'TestRunStarts|TestRunWithout' -v`
Expected: FAIL — `TestRunStartsOptionalCapability`: "Run did not invoke the orchestrator's optional Start capability" (Run does not call Start yet).

- [ ] **Step 3: Add the guarded `Start` call to `Run`**

In `core/bridge/bridge.go`, the current `Run` is:

```go
func Run(ctx context.Context, newBackend BackendFactory, orch contracts.Orchestrator, o Options) error {
	if o.HubSocket == "" {
		return errors.New("bridge requires --hub-socket (pure-runner mode)")
	}
	return runHub(ctx, newBackend, orch, o)
}
```

Replace it with:

```go
func Run(ctx context.Context, newBackend BackendFactory, orch contracts.Orchestrator, o Options) error {
	if o.HubSocket == "" {
		return errors.New("bridge requires --hub-socket (pure-runner mode)")
	}
	// G5: an orchestrator may expose an optional inactivity-curator loop via a
	// Start(ctx) method (the Learner does; the plain Curator does not). Discover
	// it by type assertion — the same optional-capability pattern used for
	// Provisioner/Locator/Deleter — so contracts.Orchestrator stays unchanged.
	// The loop is a no-op unless the idle trigger is configured, and it runs on
	// its own goroutine bound to ctx (the bridge process's root context), so it
	// is torn down automatically when the session subprocess exits.
	if starter, ok := orch.(interface{ Start(context.Context) }); ok {
		starter.Start(ctx)
	}
	return runHub(ctx, newBackend, orch, o)
}
```

`RunOneShot` (the seed path) is deliberately left unchanged — it is a one-shot, short-lived process with an explicit `Consolidate` call; an idle-poll goroutine there would outlive its single turn for no benefit.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review && GOWORK=off go test ./core/bridge/ -run 'TestRunStarts|TestRunWithout' -v`
Expected: PASS (both tests).

- [ ] **Step 5: Build the host bridge package to confirm no regression**

Run: `cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review && GOWORK=off go build ./core/bridge/...`
Expected: no output (success).

- [ ] **Step 6: Commit (LOCAL only)**

```bash
cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review && git add core/bridge/bridge.go core/bridge/bridge_g5_test.go && git commit -m "feat(host): discover orchestrator Start capability in bridge Run (G5)"
```

---

### Task 6: Release — tag orchestrator v0.1.16, bump host dep, README (MAIN-AGENT-ONLY)

> **⚠️ MAIN-AGENT-ONLY.** This task performs pushes, git tags, and `go get` against the network. Per the codex-exec delegation constraint, subagents run in a sandbox with no network and a read-only `.git`. Do NOT delegate this task to a subagent — the main agent executes every step here itself.

**Files:**
- Modify: `/home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review/go.mod` (orchestrator dep v0.1.15→v0.1.16), `go.sum`
- Modify: host README (the "Learning (the write side)" section) — locate with the grep in Step 4.

**Interfaces:**
- Consumes: the merged/pushed orchestrator commits from Tasks 1-4 (tagged v0.1.16); the host `Start` wiring from Task 5.
- Produces: orchestrator tag `v0.1.16` on the module's `master`; host `go.mod` pinning `github.com/Herrscherd/herrscher-orchestrator v0.1.16`; a README paragraph documenting the inactivity-triggered curator.

- [ ] **Step 1: Push orchestrator commits and tag v0.1.16**

First confirm the orchestrator tests pass one final time, then push the branch/commits and tag:

```bash
cd /home/shan/dev/herrscher-orchestrator && GOWORK=off go test -race ./... && git push origin HEAD && git tag v0.1.16 && git push origin v0.1.16
```

Expected: tests `ok`; branch pushed; `v0.1.16` tag created and pushed. (The orchestrator module must be a public repo so host CI can fetch it without auth — see the host-deps-must-be-public constraint.)

- [ ] **Step 2: Bump the host's orchestrator dependency to v0.1.16**

```bash
cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review && GOWORK=off go get github.com/Herrscherd/herrscher-orchestrator@v0.1.16 && GOWORK=off go mod tidy
```

Expected: `go.mod` line 13 now reads `github.com/Herrscherd/herrscher-orchestrator v0.1.16`; `go.sum` updated.

- [ ] **Step 3: Verify the host still builds and the bridge tests pass against v0.1.16**

```bash
cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review && GOWORK=off go build ./... && GOWORK=off go test ./core/bridge/...
```

Expected: build succeeds; bridge tests `ok`.

- [ ] **Step 4: Add the README "Inactivity-triggered curator" paragraph**

Find the README and its Learning section:

```bash
cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review && grep -rn "Learning (the write side)" README.md docs/ 2>/dev/null
```

Under that section, add a paragraph (adapt heading depth to the surrounding doc):

```markdown
#### Inactivity-triggered curator (G5)

Beyond the per-turn cadence (`memory.consolidate-every`), the learner can curate
a session that has gone quiet. When `MEMORY_IDLE_DAYS` (`idle-days`) is greater
than 0, each bridge process runs a background poll loop that fires `Consolidate`
out of band once **both** thresholds are met: at least `idle-days` have elapsed
since the last curator run **and** at least `MEMORY_IDLE_HOURS` (`idle-hours`,
default 2) of quiet since the last observed turn. It defaults **off**
(`idle-days = 0`). The idle run never blocks a turn: it executes on its own
goroutine and single-flights against the turn path with a non-blocking `TryLock`,
skipping a tick rather than making a turn wait. It adds no new write path — only
a second trigger for the existing, reversible `Consolidate`.
```

Also mark G5 as shipped in the memory-vs-Hermes gap note if one exists in the repo:

```bash
grep -rn "G5" /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review/docs/ 2>/dev/null
```

If a gap table lists G5 as pending, update its status to shipped in the same commit.

- [ ] **Step 5: Commit and push the host branch**

```bash
cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review && git add go.mod go.sum README.md docs/ && git commit -m "feat(host): bump orchestrator v0.1.16 (G5 inactivity curator) + README" && git push origin HEAD
```

Expected: commit created; branch pushed.

- [ ] **Step 6: Verify PR CI is green**

```bash
cd /home/shan/.superset/worktrees/de8a751a-e458-4f7c-9592-018804668a81/herrscher-memory-review && gh pr checks
```

Expected: all checks pass. If the orchestrator dep fails to fetch in CI, confirm the module + tag are public (host-deps-must-be-public). Do not merge until CI is green.

---

## Self-Review (completed by the plan author)

**1. Spec coverage:**
- §1 `DueForIdleRun` pure predicate + `lastRun` field → Task 1. ✅
- §1 stamp `lastRun` at top of Consolidate → Task 2 (`consolidateLocked`). ✅
- §2 `lastActivity` stamped by `Observe`; `Start`/`idleLoop` orchestrator-owned → Tasks 2, 3. ✅
- §2 host discovers `Start` by type assertion (optional capability) → Task 5. ✅
- §3 host wiring in `Run` (NOT `RunOneShot`) → Task 5. ✅
- §4 config `idle-days`/`idle-hours` + `SetIdle` wiring → Tasks 1, 4. ✅
- Concurrency section (`mu` + `TryLock`, non-reentrant split) → Tasks 2, 3. ✅
- Testing section (table-driven predicate, stamping, Start/idleLoop, race) → Tasks 1-3. ✅
- Release footprint (orchestrator minor v0.1.16, host bump, README) → Task 6. ✅
- Out-of-scope items (cross-session coord, prompt cache, configurable poll as Setting, backoff, persistence) → intentionally not implemented; `idlePollInterval` kept a package var (not a Setting), justified in Global Constraints. ✅

**2. Placeholder scan:** No TBD/TODO/"add error handling"/"similar to Task N" — every code step carries complete code. ✅

**3. Type consistency:** `SetIdle(sinceLastRunDays, idleHours int)`, `DueForIdleRun(now, lastActivity time.Time) bool`, `consolidateLocked(ctx)`, `Start(ctx)`, `idleLoop(ctx)`, `idleTick(ctx)`, `idlePollInterval` — names/signatures identical across Tasks 1-5. The host type assertion `interface{ Start(context.Context) }` matches the orchestrator's `func (l *Learner) Start(ctx context.Context)`. `contracts.Default.Orchestrators()` matches the enumerator used in the real `register_test.go`. ✅
