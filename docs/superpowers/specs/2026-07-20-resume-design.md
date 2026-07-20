# Session `/resume` — Design

**Date:** 2026-07-20
**Status:** Approved (brainstorming) — ready for implementation plan
**Scope:** Single spec, multi-repo (herrscher + herrscher-contracts + herrscher-claude-backend, herrscher-codex-backend, herrscher-cursor-backend)

## Problem

Working from `~/dev/Neublox` (or any dir), a herrscher session feels like it
"disappears" when you quit the TUI and come back: the tab is there but the
conversation restarts from an empty context, and there is no Claude-Code-style
`/resume` to reopen a past conversation.

Investigation of the current code establishes:

- **Session rows persist correctly.** State lives in a single global
  `state.json` (`$HERRSCHER_STATE_DIR/state.json` else `~/.config/dctl/state.json`,
  `core/host/serve.go:111`). `AddSession` writes on every create; `LoadState`
  reloads on boot; the shutdown path (`RunHub` → `<-ctx.Done()`,
  `core/host/serve.go:283`) deletes nothing; boot revives every persisted
  session (`goLive` + `sup.Start`, `core/host/serve.go:229`). A persisted named
  session is never dropped on quit — only an explicit `session close`
  (`RemoveSession`) removes it.
- **The felt "disappearance" is conversation loss, not row loss.** The backend
  child (`claude`/`codex`/`cursor`) owns the transcript, not herrscher. The
  claude backend already parses the stream-json `session_id`, tracks it as
  `sessID` ("last session id seen, for --resume on restart"), and `streamArgv`
  already appends `--resume <id>` on in-process restart
  (`herrscher-claude-backend/stream.go`). But `sessID` lives only in the
  in-memory `streamSession`: on TUI quit the process dies and the id is lost, so
  the next boot builds a fresh backend with no resume id → empty context.

## Goal

A Claude-Code-style `/resume`: reopen a past session with (a) its **visible
scrollback** and (b) its **real backend context** restored, with graceful
fallback to scrollback-only when a vendor cannot resume.

## Guiding idea

herrscher becomes the source of truth for a session's *continuity*. It persists,
per session:

1. an opaque **backend resume token** (the claude/codex internal `session_id`), and
2. a **transcript** of turns.

`/resume` recombines them: relaunch the backend with `--resume` **and** repaint
the scrollback.

## Data model

New per-session persisted state (`state.Session`):

- `ResumeToken string` — JSON `resumeToken,omitempty`. Opaque backend session id.
  Written only when it changes.
- `Archived bool` — JSON `archived,omitempty`. Marks a session that was
  closed-but-kept (see Close semantics). Absent/false ⇒ live as today. (Note:
  `sessionJSONRow` currently hardcodes `Status:"running"`; it must derive
  live/archived from this field.)

New on-disk artifact:

- **Transcript:** `<stateDir>/transcripts/<name>.jsonl`, append-only, one entry
  per line:
  ```json
  {"ts":"…","role":"user","text":"…"}
  {"ts":"…","role":"assistant","text":"…","cost":0.0123}
  ```
  `kind` field reserved for later enrichment (tool calls). Best-effort, mirroring
  `AppendParticipant` (O_APPEND, mkdir parent, never blocking). Purged on real
  delete.

**Invariants**

- Core stays platform-blind: it never names a vendor. The resume token is an
  opaque `string`, captured/consumed through a neutral seam. `purity_test.go`
  must stay green.
- Best-effort everywhere: a transcript or resume failure degrades to today's
  behavior, never breaks a turn.
- Backward compatible: existing sessions lack all new fields (`omitempty`); they
  behave exactly as today until their first new turn.

## Backend-native resume

Two-directional seam.

**IN (resume on start) — no new contract type.** `BuildBackend` already fills
`cfg.Settings["cmd"|"kind"|"dir"]` (`core/host/seed.go:120`). Add
`cfg.Settings["resume"] = sess.ResumeToken`. The claude backend reads it into
`Config.ResumeID` and passes it to `startStreamSession(..., resumeID, ...)`;
`streamArgv` already appends `--resume` when `resumeID != ""`. We are wiring an
existing path, not inventing claude behavior.

**OUT (capture the new id) — one optional neutral interface** in
herrscher-contracts:

```go
// ResumeAware is implemented by backends that can be resumed later; the host
// persists the opaque token and feeds it back via cfg.Settings["resume"].
type ResumeAware interface { ResumeToken() string }
```

`streamResponder` implements it by returning `r.sess.sessID`.

**OUT transport (the subtle part).** The backend lives in the **bridge child
process** (`sup.Start(sess)`), not the daemon that owns `state.json`. The token
rides the **existing hub event stream**, exactly like `Cost` already does: add a
`Resume string` field to `contracts.Event`. In the bridge's `runOneTurn`
(`core/bridge/hub.go`) — which holds the `contracts.Backend` — query
`resp.(contracts.ResumeAware)` after the turn and set `Resume` on the terminal
`reply{done}` event. The daemon's turn loop (`core/host/turnloop.go`,
`awaitTurn`, where it already reads `e.Cost`) folds `e.Resume` into
`state.Session.ResumeToken` via `SetResumeToken`, persisting **only on change** to
avoid `state.json` churn. `ResumeAware` (contracts) + the `Event.Resume` field
are the whole contract surface — no `BackendEvent` change.

**Why the token stays valid.** A claude stream-json `session_id` is **invariant
across a conversation**: every turn reports the same id; it only changes when a
brand-new session is started (no `--resume`). So the folded token is stable, the
supervisor can pass `--resume <token>` at boot without staleness, and a
bridge-process re-exec resumes the same id it already had.

**On boot / resume.** The supervisor's `bridgeArgs` appends
`--resume <sess.ResumeToken>` when non-empty; the bridge CLI passes it into
`BuildBackend` as `cfg.Settings["resume"]` → `Config.ResumeID`. A supervised
session comes back up already resumed. (`newSeedBackend`, the one-shot seed path,
likewise threads `sess.ResumeToken`.)

**Per vendor**

- **claude:** wire `ResumeID` into `Config`; implement `ResumeAware` (return
  `sessID`). Small.
- **codex / cursor:** same seam; implement if the CLI supports resume, else
  `ResumeToken()` returns `""` → automatic fallback to scrollback-only.

## Transcript & scrollback

**Recorder: daemon side.** The daemon already receives the bridge child's event
stream (same place the token is captured) and sees the user message when it
routes it to the session channel. Single writer, in the process that owns the
state dir → no cross-process race. Kept **separate from the learning call-journal**
(`Session.Journal`/`Consolidate`) — different purpose and lifecycle.

- **user** entry written on message delivery to the session.
- **assistant** entry written at end of turn (assembled `text` events + `result`
  cost).
- Tool calls not replayed in v1 (noise); `kind` field kept for later.

**Scrollback replay on tab open.** The terminal TUI is compiled into the daemon,
so it reads the transcript file directly. When a tab opens for a session with a
transcript, the TUI **seeds the scrollback buffer** with its entries (dimmed to
distinguish history from live) *before* live events arrive. Replay cap: the last
N turns (e.g. 200 entries) to avoid a massive redraw. No file rotation in v1 —
only the replay cap (known limitation).

**The join that makes it whole.** On open:
1. the session is revived/supervised (backend **already resumed** via
   `ResumeToken`), and
2. the TUI paints scrollback from the transcript.
→ You get the tab *and* the thread back, model context included. Clean fallback:
empty `ResumeToken` (vendor without resume) still yields scrollback.

## `/resume` picker (TUI UX)

The TUI already has a command palette and tabs. Add `/resume`, opening a
selection overlay.

**Contents:** resumable sessions sorted by last activity (last transcript `ts`),
columns: `name · project · last-activity · model/vendor · [live|archived] · ⟲ if
resume-capable (has a ResumeToken)`. Scope is **global** (like `state.json`) —
all projects, with the project column distinguishing them, so a session opened
from `~/dev/Neublox` shows even after relaunching elsewhere.

**Enter action:**
- **archived** → revive (`goLive`: backend resumed via `ResumeToken` + open tab +
  scrollback),
- **live, no tab** → open the tab with scrollback,
- **live, tab open** → focus.

**Close semantics change (the only behavior change).** Today `session close` =
`RemoveSession` (drops the row) + participant purge. To resume *ended*
conversations:

- **`session close` → detach/archive:** kill the supervised child, **keep** row +
  transcript + `ResumeToken`, mark `archived`. Leaves the live set but stays
  resumable.
- **`session close --purge` (or `session delete`) → real removal:** drop row +
  transcript file.
- **At boot,** `archived` sessions are **not** auto-supervised (else every old
  session respawns a `claude` at startup): the boot loop `goLive`/`sup.Start`
  **skips archived**; they come back only on demand via the picker. `reconcile`
  likewise leaves archived sessions non-live.

## Edge cases & error handling

- **Stale/expired token:** `--resume <bad>` fails at start → restart **without**
  resume (fresh) and **clear** `ResumeToken` in state; scrollback stays.
- **Migration:** existing sessions lack `resumeToken`/transcript/`archived` (all
  `omitempty`) → today's behavior; `main` keeps working; recording + capture
  begin at the first turn.
- **Purge:** removes the transcript file too (mirror `RemoveParticipantJournal`)
  — no leak.
- **Multi-instance:** `instanceID` already separates daemons; resuming the same
  session from two instances is out of scope for v1.
- **Transcript growth:** no rotation in v1, only the replay cap (known
  limitation).
- **Non-git / shared sessions:** transcript is keyed by **name**, independent of
  worktree → works everywhere.

## Testing

- **herrscher (this repo), unit:** transcript append/read/cap (mirror
  `journal_test.go`); boot loop skips `archived`; `close`→archive vs `--purge`;
  token persisted **only on change** (no `state.json` churn).
- **herrscher-claude-backend:** `Config.ResumeID` threaded into `streamArgv`;
  `ResumeToken()` returns `sessID`; fallback on failed `--resume`.
- **TUI:** picker render/sort; scrollback seeds the buffer; `Enter` actions.
- **Purity:** `purity_test.go` stays green — core names no vendor (token = opaque
  `string`).

## Cross-repo release ordering

1. **herrscher-contracts:** add `ResumeAware` interface + `Event.Resume` field.
   (Others depend on it.)
2. **backends** (claude first; codex/cursor stub `ResumeToken()` → `""`): consume
   the contract + `Settings["resume"]`.
3. **herrscher:** `state.Session` fields, transcript recorder, token persistence,
   boot-loop archived skip, close→archive semantics, TUI picker + scrollback.

Host CI fetches plugin deps with no auth, so every module imported in
`plugins.go` must stay a public repo (contracts + backends are already public).

## Out of scope (v1)

- Transcript rotation/compaction.
- Cross-instance resume.
- Replaying tool calls in scrollback.
- Resume for vendors whose CLI has no resume mechanism (graceful fallback only).
