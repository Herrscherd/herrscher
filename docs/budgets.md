# Budget caps

A session can be given a **spend ceiling**. When it is reached, the session **pauses**
instead of burning more of your money. Caps are checked by a gate injected into the host
turn loop. Cost caps are turn-granular: a turn that has started always finishes, because a
backend only reports what it cost once it is done. **Token caps are also enforced
mid-turn**: a turn that would spend past the ceiling is cut where it stands.

## Two units, two scopes

There are four caps. Every one of them is **`0` = uncapped**.

| Cap | Unit | Scope |
|-----|------|-------|
| `cost_cap` | USD (a float, folded from each turn's reported cost) | This session. |
| `token_cap` | Total tokens, input **+** output | This session. |
| `cohort_cost_cap` | USD | The whole cohort. |
| `cohort_token_cap` | Total tokens | The whole cohort. |

A **cohort** is the session's whole parent forest: the gate climbs `Parent` up to the root
ancestor, then walks the entire subtree from there, summing usage over every member. So a
lead session and everything it delegated share one cohort budget. The climb and the walk
both carry a visited set, so a `Parent` cycle cannot loop.

Usage is never tracked separately: the gate **re-derives it from the transcript fold** on
every check, so the transcript stays the single source of truth.

## Setting caps

At creation, on `session create`:

```text
herrscher session create --name refactor --project app \
    --cost_cap 5 --token_cap 2000000
```

Or on a live session, with `session set-budget`:

```text
herrscher session set-budget --name refactor
    [--cost_cap USD] [--token_cap N]
    [--cohort_cost_cap USD] [--cohort_token_cap N]
    [--scope session|cohort]
```

| Param | Meaning |
|-------|---------|
| `--name N` | The session to patch (required). |
| `--cost_cap USD` | USD cap (`0` = uncapped). |
| `--token_cap N` | Token cap (`0` = uncapped). |
| `--cohort_cost_cap USD` | Cohort USD cap (`0` = uncapped). |
| `--cohort_token_cap N` | Cohort token cap (`0` = uncapped). |
| `--scope session\|cohort` | `session` (default) patches this session; `cohort` patches **every member** of its cohort. |

Only the params you actually supply are patched: an omitted (or empty) flag leaves that
cap unchanged. Passing `0` explicitly is how you **uncap**.

Like every operator command, `session set-budget` is a neutral `contracts.Cmd`, so the same
verb is reachable from the CLI and from a gateway's command surface.

## What happens when a cap is hit

The gate's `Check` returns the first reason that trips, in a fixed order: session caps
before cohort caps, so a session-level trip names its own unit:

| Reason | Condition |
|--------|-----------|
| `cost` | `cost_cap > 0` and session cost **≥** cap. |
| `tokens` | `token_cap > 0` and session tokens **≥** cap. |
| `cohort_cost` | `cohort_cost_cap > 0` and cohort cost **≥** cap. |
| `cohort_tokens` | `cohort_token_cap > 0` and cohort tokens **≥** cap. |

The comparison is `>=`, so a cap trips as soon as usage *reaches* it.

The gate is consulted at **two** points in the turn loop. One call answers both questions. The reason and the token headroom come from the same transcript fold, so asking them apart
would only mean folding twice:

1. **Before opening a new turn**, when an `input` event is dequeued. If the session is over
   budget the input is **refused before any turn opens**, a `paused` event is re-emitted,
   and a status line tells the subscriber that this specific message was dropped. If it may
   run, the headroom from that same call is what the mid-turn guard below watches.

   > The refused input is **not re-queued**. There is no auto-replay path: resuming only
   > clears the paused flag, nothing re-injects the turn. Re-queueing would spin forever, so
   > the message is dropped honestly and you must **resend it** after raising the cap.

2. **After a completed turn** (on the terminal `reply{done}`). On a trip the driver calls
   `emitPaused`, which fans a `paused` event carrying the reason out through the same path
   as every other subscriber-visible frame, so a LIVE subscriber sees the pause
   immediately rather than on its next status poll. The turn ends there.

A `pick` event is deliberately **not** gated: it answers an in-flight or already-rendered
turn rather than opening a new one.

The reason is persisted as the session's `paused_reason` the moment it trips, so a session
comes back **paused across daemon reloads**. It surfaces in `session list` as
`status: "paused"`, and the caps plus the reason are stamped onto the session's usage row.

## Mid-turn: cutting a runaway turn

Checking only at boundaries leaves one hole: a *single* turn can spend arbitrarily far past
a cap, and the pause lands once the money is already gone. So token caps are also enforced
while the turn runs.

The dispatch-time `Check` also answers a **headroom**: how many tokens this turn may spend
before the tightest of the session and cohort token caps trips, i.e. the cap minus what the
transcript says is already spent, floored at zero. It is read **once per turn**: re-deriving
it per event would re-read the transcript hundreds of times a turn. A session with no token
cap comes back uncapped, and the guard stays out of the way entirely.

The bridge accumulates the backend's usage over the turn and stamps the running totals onto
every rendered event (that is what a gateway's live token counter is built from), so the
turn loop has the number it needs without asking anyone. When that counter reaches the
headroom, the driver:

- sends the **same out-of-band `interrupt` frame** `/stop` uses, and the bridge cancels the
  backend turn, which emits its terminal reply as usual, so the turn ends cleanly rather
  than being severed;
- fans **one** status event naming the cap (repeating it every chunk would flood the view);
- lets the ordinary after-turn check pause the session, since the recorded turn now puts
  usage over the cap. The after-turn `Check` stays the only writer of `paused_reason`.

Two deliberate limits:

- **Cost has no mid-turn equivalent.** A backend reports cost only in its terminal `result`
  event, so there is no number to compare a cost cap against while the turn runs.
- **A `pick` is not watched.** It answers a turn that is already rendered; cutting it would
  leave that turn unanswered.
- **A backend that reports no usage is not watched either.** Only the Claude backend streams
  token counts today; with codex or cursor the counter stays at zero, so token caps fall back
  to boundary-only enforcement. Nothing breaks: the guard simply never fires.

## Resuming

**Raising or clearing the cap is the resume mechanism.** `session set-budget` re-evaluates
the pause after applying the patch: if the session's current usage no longer exceeds the
cap that tripped (or that cap is now `0`), the paused reason is cleared and the session
is live again.

```text
# raise the ceiling and resume
herrscher session set-budget --name refactor --cost_cap 10

# or remove the ceiling entirely
herrscher session set-budget --name refactor --cost_cap 0

# a cohort-wide trip: patch the whole forest at once
herrscher session set-budget --name refactor --cohort_cost_cap 50 --scope cohort
```

The re-evaluation is per-reason: only a `paused_reason` matching the cap you changed is
cleared, so raising the session cost cap does not silently un-pause a session that is
actually stuck on a cohort token cap. Cohort reasons are re-evaluated against the cohort
totals in the same pass.

Then **resend the message that was refused**: clearing the pause does not replay it.

## Not the same as the memory node budget

`contracts.EnforceBudget` / `contracts.BudgetError` are an unrelated, differently-scoped
mechanism: they bound a **single memory node's body in runes** at `Memory.Record` time, so
a writer consolidates instead of blindly appending. A limit of `0` disables that check too,
but it has nothing to do with session spend. See
the consolidation lifecycle.

## Related

- CLI reference: the rest of the `session` verbs.
- Session lifecycle: where `paused` sits among the states.
- Inter-session coordination: where the `Parent` link a cohort is built from comes from.
