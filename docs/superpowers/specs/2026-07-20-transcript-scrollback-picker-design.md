# Transcript, scrollback replay & `/resume` picker — Design

**Date:** 2026-07-20
**Status:** Approved (brainstorming) — ready for implementation plan
**Scope:** Single repo (herrscher). No contract change: the resume seam
(`Event.Resume` + `ResumeAware`) already shipped yesterday.

## Context — what shipped yesterday

Backend-native resume is **done**: `state.Session.ResumeToken` is folded in from
each turn's `reply{done}` via `Event.Resume`, wired through `persistResume` in
`awaitTurn` (`core/host/turnloop.go:316`) and `SetResumeToken` in the hub
(`core/host/hub.go:72`). A supervised session already comes back `--resume`d on
restart from `sess.ResumeToken`. The originally-planned resume **sidecar** was
**not** used — the token rides `Event.Resume` instead. This matters here: the
transcript recorder attaches at the *same* daemon-side point, so there is no
cross-process file to reconcile.

What is still missing, and what this spec covers:

1. a durable **transcript** of turns per session,
2. **scrollback replay** when a session's TUI tab opens, and
3. a **`/resume` picker** with **archive** (close-but-keep) semantics.

## Goal

Reopen a past session with (a) its **visible scrollback** and (b) its **real
backend context** (already restored via `ResumeToken`), with graceful fallback
to scrollback-only when a vendor cannot resume.

## Invariants

- **Core stays platform-blind.** No vendor named anywhere new; the resume token
  is already an opaque `string`. `purity_test.go` stays green.
- **Best-effort everywhere.** A transcript, replay, or resume failure degrades to
  today's behavior and never breaks a turn.
- **Backward compatible.** Existing sessions lack `archived` and have no
  transcript file (all `omitempty`); they behave exactly as today until their
  first new turn.

## Data model

**`state.Session`** gains one field:

- `Archived bool` — JSON `archived,omitempty`. A session closed-but-kept (see
  Archive semantics). Absent/false ⇒ live as today.
  `sessionJSONRow` currently hardcodes `Status:"running"`
  (`core/internal/manager/session.go:61`); it must derive `running` vs `archived`
  from this field.

**On-disk artifact — transcript.** `<stateDir>/transcripts/<name>.jsonl`,
append-only, one entry per line:

```json
{"ts":"2026-07-20T10:00:00Z","role":"user","text":"…"}
{"ts":"2026-07-20T10:00:03Z","role":"assistant","text":"…","cost":0.0123}
```

`kind` field reserved for later enrichment (tool calls); unused in v1. Kept
**separate** from the learning call-journal (`Session.Journal`/`Consolidate`) —
different purpose and lifecycle. Purged on real delete.

## Storage API — `core/internal/state/transcript.go`

Mirrors `journal.go` (best-effort observability, never errors a turn):

```go
type TranscriptEntry struct {
    Ts    string  `json:"ts"`
    Role  string  `json:"role"`           // "user" | "assistant"
    Text  string  `json:"text"`
    Cost  float64 `json:"cost,omitempty"`
    Kind  string  `json:"kind,omitempty"` // reserved (tool calls)
}

func TranscriptPath(dir, name string) string          // dir/transcripts/<name>.jsonl
func AppendTranscript(path string, e TranscriptEntry) error // O_APPEND, mkdir parent
func ReadTranscript(path string, cap int) []TranscriptEntry // last `cap` entries (0 = all)
func RemoveTranscript(path string) error              // missing file is not an error
```

`AppendTranscript` marshals one JSON line, opens `O_CREATE|O_WRONLY|O_APPEND`,
mkdir-parents, and swallows nothing louder than an error return the caller
ignores. `ReadTranscript` scans line-by-line, unmarshals, and keeps only the last
`cap` entries (bounded replay).

## Recorder — daemon-side (`turnloop.go`)

The daemon owns the state dir and already sees both sides of a turn, so it is the
single writer — no cross-process race:

- **user** entry: in `pump`, at the `{T:"human", Who, Text}` fan-out
  (`turnloop.go:250`).
- **assistant** entry: in `awaitTurn`, at `reply{done}` — `e.Text` and `e.Cost`
  are already on the event (`turnloop.go:315`).

`sessionDriver` gains a `record func(state.TranscriptEntry)` field, wired in
`RunSession` exactly like `persistResume`, `nil` in the operator-CLI path and
tests (so no file is written there). Timestamps use `time.Now().UTC()`.

Tool calls are not recorded in v1 (noise); the `kind` field is the forward hook.

## Archive semantics — a new verb, `close` unchanged

Per the decision to avoid regressing `close`:

- **`session close <name>` — unchanged: real removal.** Still stops the child,
  removes the worktree, archives the channel, and drops the row
  (`RemoveSession`). It additionally now purges the transcript and participants
  journal so nothing leaks: add `state.RemoveTranscript(state.TranscriptPath(...))`
  next to the existing `RemoveParticipantJournal` call
  (`core/internal/manager/session.go:297`).
- **`session archive <name>` — new verb: close-but-keep.** Stops the supervised
  child (`sup.Stop`), archives the channel (same `adminFor(sess).Archive` call),
  and **keeps** the row + transcript + `ResumeToken`, setting `Archived=true`
  (new `state.SetArchived(name, true)`, persist-on-change like
  `SetResumeToken`). It does **not** remove the worktree (the branch/work is
  kept, resumable). Registered in `commands.go` alongside `close`.

**Boot & reconcile skip archived.** The boot loop (`serve.go:242`) and
`hub.reconcile` (`hub.go:110`) must `continue` past `Archived` sessions — else
every old session respawns a `claude` at startup. `goLive` itself is **not**
hardened against archived (the picker's revive path calls it), so the skip lives
in the two callers that iterate all persisted sessions.

## Scrollback replay (TUI)

When a session's tab is first created, seed `tab.lines` from its transcript
(dimmed, distinguishing history from live) **before** any live event arrives.
Replay cap: the last ~200 entries (bounded redraw); no rotation in v1.

The `tui` package stays free of a `state` dependency, so the read crosses the
existing `Backend` seam:

```go
// Backend gains:
Scrollback(name string) []ScrollbackLine  // pre-dimmed, capped history lines
```

`ScrollbackLine` is a small neutral struct owned by the `tui` package,
`{Role, Text string}` — the TUI applies the dim style itself, keeping rendering
consistent with live events. The terminal gateway implements `Scrollback` by
calling `state.ReadTranscript(TranscriptPath(dir,name), 200)` and mapping each
entry to a `ScrollbackLine` (`user`→the human-line style, `assistant`→the reply
style), which the TUI then renders dimmed. `ensureTab` (`tui.go:175`) seeds the new tab from `Scrollback` on first
creation; subsequent live events append as today.

## `/resume` picker (TUI)

A new palette overlay listing **resumable** sessions, global scope (like
`state.json`) so a session opened from `~/dev/Neublox` shows even after
relaunching elsewhere.

- **Contents:** resumable sessions (live + archived) sorted by last activity
  (last transcript `ts`), columns: `name · project · last-activity ·
  model/vendor · [live|archived] · ⟲ if resume-capable (non-empty
  ResumeToken)`.
- **Enter action:**
  - **archived** → revive: unarchive + `goLive` + `sup.Start` (backend resumed
    via `ResumeToken`) + open tab + scrollback,
  - **live, no tab** → open the tab + scrollback,
  - **live, tab open** → focus.

Seams (extend `Backend`):

```go
Resumable() []ResumeInfo   // name, project, lastActivity, vendor, archived, resumable
Resume(name string) error  // revive an archived session (hub-side)
```

The hub's `Resume` clears `Archived` (`SetArchived(name,false)`), then `goLive` +
`sup.Start` — the same pair the boot loop runs for a live session. `Resumable`
reads the session list + each transcript's last `ts`.

## Edge cases & error handling

- **Stale/expired token** is already handled by yesterday's resume path (a failed
  `--resume` starts fresh); scrollback still paints.
- **Migration:** sessions predating this change have no `archived`/transcript
  (`omitempty`) → today's behavior; recording begins at the first new turn.
- **Purge/close leak:** `close` now removes the transcript **and** the
  participants journal — no orphaned files.
- **Transcript growth:** no rotation in v1, only the ~200-entry replay cap (known
  limitation).
- **Non-git / shared sessions:** transcript is keyed by **name**, independent of
  worktree → works everywhere.
- **Multi-instance:** `instanceID` already separates daemons; resuming the same
  session from two instances is out of scope for v1.

## Testing

- **state (unit):** transcript append/read/cap (mirror `journal_test.go`);
  `RemoveTranscript` on missing file is a no-op; `SetArchived` persists only on
  change.
- **host (unit):** recorder writes a user + assistant entry per turn (driver test
  with a fake `record`); boot loop / `reconcile` skip `archived`; `close` purges
  transcript + participants; `archive` keeps row + transcript + token and sets
  `Archived`.
- **manager (unit):** `sessionJSONRow` derives `archived` vs `running`; the new
  `archive` verb is registered and routes.
- **TUI:** picker render/sort (by last activity); `ensureTab` seeds scrollback
  from `Scrollback`; Enter actions (archived→revive, live-no-tab→open,
  live-tab→focus).
- **Purity:** `purity_test.go` stays green — core names no vendor.

## Out of scope (v1)

- Transcript rotation/compaction.
- Cross-instance resume.
- Replaying tool calls in scrollback.
- Resume for vendors whose CLI has no resume mechanism (graceful
  scrollback-only fallback).

## Implementation order (single repo)

1. `state`: `Session.Archived`, `SetArchived`, `transcript.go`
   (`TranscriptEntry` + append/read/remove/path).
2. `turnloop.go` + `hub.go`: `record` field wired like `persistResume`; user +
   assistant entries.
3. `serve.go` + `hub.reconcile`: skip `Archived`.
4. `manager/session.go` + `commands.go`: `archive` verb; `close` purges
   transcript; `sessionJSONRow` status derivation.
5. TUI: `Backend.Scrollback` + seed in `ensureTab`; `/resume` picker overlay +
   `Backend.Resumable`/`Resume` + hub revive.
