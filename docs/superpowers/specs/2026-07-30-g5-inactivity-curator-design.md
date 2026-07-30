# G5 — Inactivity-triggered curator — design

**Date:** 2026-07-30
**Status:** Design (approved 2026-07-30)
**Slice:** G5 of the Hermes-parity roadmap
(`docs/superpowers/specs/2026-07-30-memory-learning-hermes-parity-design.md`)
**Repos:** `herrscher-orchestrator` (trigger policy), host (`core/bridge`,
`core/host`) (turnloop/supervisor wiring). No `herrscher-contracts`,
no `herrscher-obsidian-memory` change (see §4).

## Goal

Today the *only* trigger for `Learner.Consolidate` is the per-turn cadence:
`Observe` counts turns and fires every `memory.consolidate-every` turns
(`learner.go:93-102`). A session that goes quiet — the channel sits idle for
hours or days — never gets curated again until someone speaks. Hermes' parity
gap is the background fork: `>=7 days since last curator run AND idle >= 2h` →
run curation out-of-band with its own prompt cache, independent of user
traffic. G5 adds the same idea here: a clock-driven predicate that, given the
timestamp of the last Consolidate and the timestamp of the last observed
activity, says whether a run is due — plus the host-side wiring that checks it
periodically and fires `Consolidate` off the turn path. It only starts to
matter once G3 (staleness) and G4 (reversible archive) give the sweep/merge
passes real work to find when a session has been silent for a while.

## Baseline (verified in code, 2026-07-30)

- `Learner.Observe` (`learner.go:91-102`) is the only Consolidate trigger
  today: `if l.every > 0 { l.n++; if l.n%l.every == 0 { _ = l.Consolidate(ctx) } }`.
  It fires from inside the turn path, synchronously, but its result is
  swallowed (`_ =`) — the existing invariant that learning never breaks a
  turn is upheld by ignoring the error, **not** by running off-goroutine.
- `Curator` (`orchestrator.go:29-38`) already carries an injectable clock:
  `now func() time.Time`, defaulted to `time.Now` in `NewScoped`, consumed by
  `Sweep` (`sweep.go:28`) via `c.now()`. `Learner` embeds `*Curator`, so `l.now()`
  is already available to a G5 predicate — no new clock plumbing needed.
- `register.go:24-56` is the factory: it builds either a plain `Curator` or,
  when an extractor is configured, a `Learner` via `NewLearner(...)`, reads
  `memory.consolidate-every` into `every`, and calls setters (`SetStaleness`,
  `SetMerge`) for each opt-in feature. G5 follows the same shape: a new
  `SetIdle` setter, two new bare `Setting` keys.
- `contracts.Orchestrator` (`herrscher-contracts/orchestrator.go`) exposes only
  `Context`, `Observe`, `Consolidate` (via `CurationHook`), `Close`. Nothing
  session-scoped is exposed beyond that — the host cannot see the Learner's
  turn counter or clock, and is not meant to (ports only, policy in
  orchestrator).
- **The host process topology is the key fact for wiring.** There is no
  central daemon that holds `contracts.Orchestrator` instances. Confirmed by
  reading the host: one `Orchestrator` is built **per session bridge
  subprocess** in `core/bridge/bridge.go` (`orch` param threaded into
  `bridge.Run` / `runHub`, `core/bridge/hub.go`), and each session runs as its
  own OS process supervised by `core/internal/supervisor`. The daemon hub
  (`core/host/hub.go`) only holds `state.Session` structs and subprocess
  handles/sockets — it never touches `contracts.Orchestrator` directly. So an
  inactivity check has to live **inside the bridge process**, next to the
  `orch` it already owns, not in the host daemon.
- **No last-activity timestamp exists anywhere.** Grepped the whole host tree
  for `LastActiv`/`lastActiv`/`LastSeen`/`lastSeen`/`LastTurn`/`idleSince`:
  zero matches. `state.Session` carries none. G5 must introduce one.
- **An existing ticker-loop pattern to copy.** `core/host/loops.go` has
  `pingLoop` (30s ticker) and `statusLoop` (60s ticker), each a
  `for { select { case <-ctx.Done(): return; case <-t.C: ... } }` goroutine
  started alongside the other daemon loops. These live in the *daemon*, not
  the bridge — G5's loop is analogous in shape but must live in
  `core/bridge/hub.go`'s `runHub`, the one place that already has both `orch`
  and the per-turn event stream to timestamp.
- `Learner` doc comment (`learner.go:51-53`): "a `Learner` is single-goroutine
  per session: Consolidate runs synchronously from Observe on the turn path,
  so pending/seen/offset are intentionally not mutex-guarded." Any new caller
  of `Consolidate` from a second goroutine breaks this assumption unless
  serialized (see §5).

## Design

### 1. `DueForIdleRun` — pure, clock-injected predicate on `*Learner`

New field on `Learner`: `lastRun time.Time`, stamped at the end of every
`Consolidate` call (turn-path *and* idle-triggered — both count as "a
curator run happened").

```go
// idleDays/idleHours configure the G5 inactivity trigger (SetIdle).
// idleDays <= 0 disables the trigger entirely (opt-in, default off).
idleDays  int
idleHours int
lastRun   time.Time // stamped at the end of every Consolidate (zero = never run)
```

```go
// SetIdle configures the G5 inactivity trigger. sinceLastRunDays <= 0 disables
// it (opt-in, default off); idleHours is the quiet-period threshold measured
// against lastActivity. Mirrors SetStaleness/SetMerge's setter shape.
func (l *Learner) SetIdle(sinceLastRunDays, idleHours int)

// DueForIdleRun reports whether an inactivity-triggered Consolidate should
// fire, given the current time and the timestamp of the last observed
// activity (a turn, from the host's point of view). Pure and deterministic —
// takes no clock reading itself, so it is unit-testable with fixed times and
// needs no real timers. Mirrors Hermes: (>= sinceLastRunDays since lastRun)
// AND (>= idleHours since lastActivity). false when the trigger is disabled
// (idleDays <= 0), when lastRun is zero (never consolidated: the per-turn
// cadence or an explicit first run should establish a baseline before idle
// checks kick in — avoids firing on a session that has simply never talked
// long enough to hit `consolidate-every`), or when either threshold is not
// yet met.
func (l *Learner) DueForIdleRun(now, lastActivity time.Time) bool
```

`now` is passed in explicitly rather than read via `l.now()` — the caller
(host bridge) is the one that knows wall-clock time and drives the poll tick;
keeping it an explicit parameter (not `l.now()` internally) makes the method
trivially testable without touching the injected `Curator.now` field, and
keeps the predicate's contract self-contained: two timestamps in, one bool
out.

Stamping `lastRun`: add one line at the top of `Consolidate` (`learner.go:107`,
right after the nil-guard) — `l.lastRun = l.now()` — so both trigger paths
(per-turn cadence and idle) update it uniformly, and a manual/test-only
`Consolidate()` call (no trigger involved) also counts as "ran," matching
Hermes semantics (the *fact* that curation ran resets the inactivity clock,
regardless of who asked for it).

### 2. No contracts change — reuse `Consolidate` + a host-visible predicate

`contracts.Orchestrator` is unchanged. The host does not need a new port
method: it already holds a `contracts.Orchestrator` value (the interface) in
`runHub`'s `orch` parameter, but that's typed as the *interface* — it cannot
see `DueForIdleRun` or `SetIdle`, which live on the concrete `*Learner`.

Decision: **the idle check happens where the concrete type is still known —
at construction time, in the same factory closure that already builds the
`Learner` and calls `SetStaleness`/`SetMerge`.** Concretely, `register.go`'s
`Orchestrator` factory returns `contracts.Orchestrator`, so the host-side
`buildOrchestrator` (`core/bridge/bridge.go` call site, per the host survey)
gets back the *interface* — it cannot type-assert its way to `DueForIdleRun`
either, by design (ports only). So the poll-and-fire logic cannot live
outside the orchestrator package as host code calling a Learner method
directly.

Two ways to reconcile this without a contracts change:

- **(a) Orchestrator-internal timer, no host loop at all:** `Learner` starts
  its own background goroutine (in `NewLearner` or a `Learner.Start(ctx)`)
  that ticks, calls `l.DueForIdleRun(l.now(), l.lastActivity)`, and fires
  `Consolidate` itself. This needs the Learner to *know* about
  `lastActivity`, which today it does not track (only `Observe` sees turns,
  and does not stamp a "last observed at" field).
- **(b) Host polls via `Observe`'s own return, no new port method:** Reuse
  `Observe`, which already runs on every turn and already knows the
  wall-clock moment a turn happened (it's called right after the turn). Have
  `Learner.Observe` stamp `l.lastActivity = l.now()` itself (it's the
  activity signal), and separately have `Learner` expose the idle check as
  part of Observe's own internal bookkeeping rather than a host-driven poll.

**Chosen: (a) — an internal, orchestrator-owned idle loop**, for one reason:
the host survey found **zero** existing "last activity" concept and **no**
per-session host-side registry to hang a poll loop on (each session is an
isolated OS subprocess; there is no central place in the daemon that both
knows wall-clock idle time *and* holds a typed `*Learner`). Introducing a
host-side timestamp registry purely to route "is it idle" questions back into
a package that already has a clock and could track its own last-activity
field is the more roundabout of the two options, and forcing the host to type
switch on the concrete orchestrator would violate "ports only, policy not
engine" from the other direction (host reaching into orchestrator internals).
Instead:

- `Learner` gains `lastActivity time.Time`, stamped by `Observe` (the one
  place that already sees every turn) at the top of the method, alongside the
  existing `Curator.Observe` call.
- `Learner` gains a `Start(ctx context.Context)` method the host calls once,
  right after constructing the orchestrator in `core/bridge/bridge.go`
  (`Run`/`RunOneShot`, next to where `orch` is received) — mirroring the shape
  of `core/host/loops.go`'s `pingLoop`/`statusLoop` (ticker + select on
  `ctx.Done()`), but living in `herrscher-orchestrator`, not the host, since
  it only touches Learner-internal state:

```go
// Start runs the G5 inactivity poll loop until ctx is cancelled. It is a
// no-op if the idle trigger is disabled (SetIdle never called or
// sinceLastRunDays <= 0). The host calls this once per bridge process,
// right after constructing the orchestrator; it never blocks a turn — the
// loop only ever calls Consolidate from its own goroutine, single-flighted
// against the turn path (see §5).
func (l *Learner) Start(ctx context.Context) {
	if l.idleDays <= 0 {
		return
	}
	go l.idleLoop(ctx)
}

func (l *Learner) idleLoop(ctx context.Context) {
	t := time.NewTicker(idlePollInterval) // e.g. 10 * time.Minute; well under idleHours granularity
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// Read lastActivity/lastRun under a brief Lock just to evaluate the
			// predicate — this is quick and always safe to wait for; it is the
			// *following* TryLock (guarding Consolidate itself) that must never
			// block (see "Concurrency / out-of-band safety" below).
			l.mu.Lock()
			due := l.DueForIdleRun(l.now(), l.lastActivity)
			l.mu.Unlock()
			if !due {
				continue
			}
			if !l.mu.TryLock() {
				// A turn-path Consolidate currently holds the lock. The idle
				// path NEVER blocks waiting for it (invariant 2): skip this
				// tick and retry at the next one — lastActivity/lastRun will
				// be re-evaluated fresh next time.
				continue
			}
			_ = l.consolidateLocked(ctx) // best-effort; error swallowed, same as Observe's cadence path
			l.mu.Unlock()
		}
	}
}
```

The host's only change: one call, `orch.(interface{ Start(context.Context) }).Start(ctx)`
— guarded by a type-assertion (mirrors the existing `Provisioner`/`Locator`
optional-capability pattern in `herrscher-contracts/memory.go`) so a plain
`Curator` (no idle trigger) or a future non-Learner orchestrator is
unaffected. This keeps `contracts.Orchestrator` untouched: `Start` is an
**optional capability**, discovered by type assertion at the one call site in
`core/bridge/bridge.go`, exactly like `Provisioner`/`Locator`/`Deleter` are
discovered elsewhere in the codebase. No contracts release needed.

### 3. Host wiring point (exact)

`core/bridge/bridge.go`, in `Run` (used by the live hub path) — after
`runHub`'s prerequisites are known but the natural spot is at the top of `Run`,
before dispatch:

```go
func Run(ctx context.Context, newBackend BackendFactory, orch contracts.Orchestrator, o Options) error {
	if o.HubSocket == "" {
		return errors.New("bridge requires --hub-socket (pure-runner mode)")
	}
	if starter, ok := orch.(interface{ Start(context.Context) }); ok {
		starter.Start(ctx) // G5: no-op unless the idle trigger is configured
	}
	return runHub(ctx, newBackend, orch, o)
}
```

`ctx` here is the bridge process's root context (cancelled on process exit),
so `idleLoop`'s goroutine is cleaned up automatically when the session
subprocess is torn down by the supervisor — no separate shutdown hook needed.
`RunOneShot` (the seed path, `core/host/seed.go`) is deliberately **not**
wired: it is a one-shot, short-lived process with an explicit `Consolidate`
call already (`seed.go:190`); starting an idle-poll goroutine there would
outlive the process's single turn for no benefit.

### 4. Config surface: no contracts change, orchestrator-only

Two new bare `Setting`s in `register.go`'s manifest, read in the Learner
branch of the factory and pushed via `SetIdle` — same shape as G2's
`merge-min-nodes`/`merge-target`/`merge-max`:

| Setting key | Env | Default | Meaning |
|---|---|---|---|
| `idle-days` | `MEMORY_IDLE_DAYS` | `0` (off) | days since the last Consolidate run before the idle trigger may fire; `0` disables G5 cleanly. |
| `idle-hours` | `MEMORY_IDLE_HOURS` | `2` | hours of quiet (no observed turn) required, once `idle-days` has elapsed, before firing. Only consulted when `idle-days > 0`. |

```go
// register.go, Learner branch, alongside SetStaleness/SetMerge:
idleDays, _ := strconv.Atoi(cfg.Get("idle-days"))
idleHours, _ := strconv.Atoi(cfg.Get("idle-hours"))
l.SetIdle(idleDays, idleHours)
```

```go
// SetIdle configures the G5 inactivity trigger. sinceLastRunDays <= 0
// disables it (opt-in, default off).
func (l *Learner) SetIdle(sinceLastRunDays, idleHours int) {
	l.idleDays = sinceLastRunDays
	l.idleHours = idleHours
}
```

Default OFF matches the roadmap slice's explicit requirement ("default OFF (0
disables)"). No `herrscher-contracts` `Setting` shape change — this reuses
the existing `contracts.Setting{Key, Env, Help}` pattern already in
`register.go`.

## Concurrency / out-of-band safety

This is the sharp edge the roadmap slice calls out explicitly: the Learner
doc says `pending`/`seen`/`offset` are **not** mutex-guarded, because
`Consolidate` today only ever runs synchronously from `Observe` on the turn
path (single-goroutine-per-session). G5 introduces a second caller
(`idleLoop`'s goroutine) that can call `Consolidate` concurrently with a
turn's own `Observe → Consolidate` — a straightforward data race on
`pending`/`seen`/`offset`/`n`/`lastRun`/`lastActivity` if left unguarded.

**Resolution: `sync.Mutex` + non-blocking `TryLock` on the idle path only.**
This is RESOLVED in favor of non-blocking `TryLock`, not a blocking mutex —
invariant 2 ("learning never breaks a turn") forbids the turn path ever
waiting on the background curator, and a plain blocking `Lock` on the idle
side would let a slow turn-path `Consolidate` stall the idle goroutine (harmless)
but, more importantly, would let a slow *idle-triggered* `Consolidate` force a
concurrent turn-path `Observe` to block on `Lock` until the idle run finishes —
exactly the dependency invariant 2 rules out. Entirely inside `Learner`, no
host involvement, no change to the "ports only" boundary:

- Add `mu sync.Mutex` to `Learner`, guarding `lastActivity`, `lastRun`, `n`,
  and the **entire body** of `Consolidate` (not just the trigger check), so a
  turn-path call and an idle-loop call never interleave.
- **Turn path:** `Observe`'s existing call to `l.Consolidate(ctx)` on the
  cadence path takes `mu.Lock()` normally (a plain blocking lock is fine
  here — it is the turn's own work, already synchronous and already-accepted
  cost; the turn path is not waiting on the curator, it *is* the curator in
  this call).
- **Idle path:** `idleLoop` never calls `mu.Lock()` to run Consolidate. It
  calls `mu.TryLock()` first. If it succeeds, the idle goroutine now holds the
  lock exactly as a turn-path call would and runs `Consolidate` under it. If
  `TryLock` fails — a turn-path `Consolidate` currently holds the lock — the
  idle run is **skipped this tick** and retried at the next tick; it never
  blocks waiting for the lock to free up.
- This makes the "never overlap" property structural: a turn-path
  `Consolidate` and an idle-triggered one can never run concurrently, and the
  *only* party that ever blocks acquiring the lock is another turn-path call
  (the same cost the turn path already pays today) — the idle path degrades
  to "try again next tick," never to "wait."
- **Turn-loop-unaffected invariant preserved:** the idle loop's `Consolidate`
  call runs from `idleLoop`'s own goroutine (§2), fired asynchronously by the
  host's `Start` call, never inline with a turn's request/response path; and
  because it uses `TryLock`, it can never impose a wait on a turn-path caller
  either. The one shared resource (the mutex) never puts the turn path in a
  position of waiting on the background curator.

## Testing (orchestrator, fake clock, no real timers)

- **`DueForIdleRun` unit tests**, table-driven over `(now, lastRun,
  lastActivity, idleDays, idleHours)`:
  - disabled (`idleDays <= 0`) → always `false`, regardless of times.
  - `lastRun` zero (never consolidated) → `false` (no baseline yet).
  - `sinceLastRun < idleDays` → `false` even if `lastActivity` is ancient.
  - `sinceLastRun >= idleDays` but `idle < idleHours` (recent turn) → `false`.
  - both thresholds met → `true`.
  - boundary equality (`sinceLastRun == idleDays`, `idle == idleHours`) →
    `true` (inclusive, matches Hermes' `>=`).
- **`lastRun` stamping:** a fake clock advanced across two `Consolidate`
  calls; assert `lastRun` updates each time, including a manual/no-trigger
  call.
- **`lastActivity` stamping:** `Observe` with a fake clock; assert
  `lastActivity` updates on every call regardless of whether the per-turn
  cadence also fires.
- **`Start`/`idleLoop` integration (fake clock + a controllable ticker or a
  `Consolidate` spy):**
  - trigger disabled → `Start` returns immediately, no goroutine spawned
    (assert via a spy that `Consolidate` is never called after advancing the
    fake clock far past any threshold).
  - trigger enabled, thresholds met at tick time → spy sees exactly one
    `Consolidate` call.
  - trigger enabled, thresholds not met → spy sees zero calls across several
    ticks.
  - `ctx` cancellation stops the loop (goroutine leak check / spy stops
    receiving calls after cancel).
- **Concurrency/race test:** run `-race`; drive concurrent `Observe` (turn
  path, cadence-triggered `Consolidate`) and a forced idle-triggered
  `Consolidate` against the same `Learner` from two goroutines; assert no
  race and that `pending`/`seen`/`offset` end in a consistent state (e.g. via
  a fake `Memory` counting writes, or simply a clean `-race` run as the
  acceptance bar — this repo does not currently unit-test for absence of a
  race beyond `go test -race`, so this test's job is to *exercise* the
  interleaving, not assert a specific internal invariant beyond "no crash, no
  race, no lost pending candidate").
- **Turn-loop-unaffected acceptance test (per roadmap Accept criterion):** a
  simulated clock/idle scenario (fake `now`, fake `lastActivity`) drives
  `DueForIdleRun` to `true`, `idleLoop` (with the ticker interval shortened
  for the test, or invoked directly as a function rather than via the real
  `time.Ticker`) fires `Consolidate` from its own goroutine, and a concurrent
  fake turn (`Observe`) proceeds and returns without observable delay beyond
  the mutex hold time already accepted for the existing cadence path.

## Release footprint

- `herrscher-orchestrator` → next minor (idle trigger + `Start` optional
  capability + config + mutex). Depends only on already-released contracts
  v0.2.9 — **no contracts release needed**.
- host: bump the orchestrator dependency; add the one guarded `Start` call in
  `core/bridge/bridge.go`'s `Run`. `RunOneShot` (seed path) unchanged. No
  `herrscher-obsidian-memory` change.
- README "Learning (the write side)" gains an **Inactivity-triggered
  curator** paragraph; the memory-vs-hermes gap note marks G5 shipped.

## Out of scope (YAGNI)

- **Cross-process / cross-session idle coordination** — each bridge
  subprocess runs its own independent idle loop against its own `Learner`
  instance; there is no host-wide "which sessions are idle" dashboard or
  batching. If the host ever wants observability into idle-triggered runs
  (e.g. surfaced in `statusLoop`'s health snapshot), that is a separate,
  later slice — G5 ships the trigger, not the telemetry.
- **A distinct prompt cache / background-fork execution model** — Hermes'
  reference forks a whole background process with its own prompt cache; here
  the "fork" is simply a goroutine calling the same `Consolidate` the turn
  path already uses. No separate execution context, cache, or resource
  budget is introduced — Consolidate is already out-of-band-safe by
  construction (best-effort, error-swallowing).
- **Configurable idle-poll interval** — `idlePollInterval` (the ticker
  granularity, e.g. 10 minutes) is a package constant, not a `Setting`. It
  only needs to be finer than `idle-hours`' granularity to detect the
  threshold promptly; exposing it as config is unnecessary knob-turning until
  a real need for a different granularity appears.
- **Backoff/jitter on the idle loop** — a single ticker firing every
  `idlePollInterval` and mostly finding `due == false` is cheap (one
  `DueForIdleRun` call, no I/O); no jitter/backoff scheme is warranted at
  this scale.
- **Persisting `lastRun`/`lastActivity` across process restarts** — both live
  in-memory on the `Learner` (mirrors `offset`/`seen`/`pending`, already
  documented as session-lifetime, non-durable state). A bridge subprocess
  restart resets the idle clock; the next `Observe` re-establishes
  `lastActivity` and the per-turn cadence remains the durable fallback
  trigger regardless. Not needed for the Accept criterion (simulated
  clock/idle triggers Consolidate off the turn loop) and out of scope here.

## Invariants (from the umbrella roadmap)

1. **Ports only, policy not engine** — no `contracts.Orchestrator` method
   added; `Start` is an optional capability discovered by type assertion at
   one host call site, the same pattern already used for
   `Provisioner`/`Locator`/`Deleter`. The trigger policy (`DueForIdleRun`) is
   a pure function living entirely in `herrscher-orchestrator`; the engine
   (`Consolidate`) is unchanged.
2. **Learning never breaks a turn** — the idle-triggered `Consolidate` runs
   from its own goroutine (`idleLoop`), started once by the host and never
   inline with a turn's request/response path; its result is discarded
   exactly like the existing per-turn-cadence call. The one point of contact
   with the turn path is a mutex whose hold duration is bounded by
   Consolidate's already-accepted cost, not a new blocking dependency.
3. **Reversible over destructive** — G5 adds no new write path: it only adds
   a second *trigger* for the existing, already-reversible `Consolidate`
   (Sweep/Merge from G2-G4 already write reversibly). Nothing about this
   slice touches how data is written, only when.
