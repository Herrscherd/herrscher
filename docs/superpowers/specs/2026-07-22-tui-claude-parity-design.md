# TUI Claude Parity — Design Spec

**Date:** 2026-07-22
**Status:** Approved (design)
**Scope:** Redesign the terminal gateway TUI (`plugins/terminal/tui`) to be a faithful copy of the Claude Code interface — both the sober visual look and the feature-parity ergonomics — plus two cross-repo additions (live token counter, turn interrupt) that Claude's live feedback depends on.

## 1. Goal & Non-Goals

### Goal
Make `herrscher`'s terminal TUI look and behave like Claude Code's terminal, while keeping Herrscher's underlying power (memory, multi-backend orchestration, multi-session). A user who knows Claude Code should feel at home: `/` slash autocomplete, `@` file mentions, `?` shortcuts, `●`/`⎿` tool lines, allow/deny permission prompts, a live spinner with elapsed timer + token counter + cost, and `esc` to interrupt a running turn.

### Non-Goals
- Herrscher-internal orchestration/memory behavior is unchanged. This is a presentation + two-signal effort, not a re-architecture.
- **Tier 3 (out of scope):** Claude-internal mode indicators (plan mode, accept-edits), model switcher, and full tool-argument expansion. These live inside the `claude` subprocess and are not surfaced usably through the stream-json seam the gateway observes.
- No new Herrscher features beyond parity (no analytics, no new commands beyond the session/help/clear set).

## 2. Feature Parity Matrix (grounding)

The terminal gateway observes the session bus via `contracts.Event` (`event.go`). Tool calls arrive as `status`/`tool` events (text + tool name + one detail field), cost arrives on the terminal `reply`, and interactive choices via `PendingChoice`/`ChoiceAware` (`backend.go`). This determines what is free vs. what needs plumbing.

| Claude feature | Tier | Where it lives |
| --- | --- | --- |
| Full-width message flow (no enclosing card) | 1 | `tui.go View()`, `render.go` |
| `>` dim user echo, bare agent prose | 1 | `render.go` |
| `●` / `⎿` tool + tool-result lines | 1 | `render.go` (from existing `tool`/`Detail`) |
| `/` slash-command autocomplete (inline) | 1 | `palette.go` |
| `@` file-mention autocomplete | 1 | new `mention.go` in tui |
| `?` shortcuts panel | 1 | `tui.go` |
| `↑` input history, `⌥⏎` newline | 1 | `tui.go` |
| Allow/deny permission menu (Claude style) | 1 | `picker.go`/`tui.go` (from `PendingChoice`) |
| Cost + elapsed timer in spinner | 1 | `tui.go` (cost already on reply) |
| Invisible multi-session (no tab bar; `/session switch`) | 1 | `tui.go`, `picker.go` |
| **Live token counter** (`↑ 3.4k tokens`) | 2 | claude-backend + contracts + herrscher + tui |
| **`esc` to interrupt a running turn** | 2 | contracts + herrscher + claude-backend |
| Plan/accept-edits mode indicators, model switcher | 3 | out of scope (not observable) |

## 3. Architecture Context (as-is, verified)

- **Daemon** (`core/host`) owns gateway I/O and supervises, per session, a child `herrscher bridge` process (`core/internal/supervisor/supervisor.go`, `exec.CommandContext`).
- **Bridge** (`core/bridge/hub.go`) is a model-agnostic turn runner calling an injected `contracts.Backend`. It maps `BackendEvent` → bus `Event` in `emitBackendEvent` (`hub.go:126`) and stamps the terminal reply (with `Cost`, `Resume`) in `runOneTurn` (`hub.go:93`). Inbound frames it accepts today: `input`, `pick` (`hub.go:62-67`).
- **Backend** for Claude is the external module `github.com/Herrscherd/herrscher-claude-backend` (`stream.go`): runs `claude -p --input-format stream-json --output-format stream-json --verbose`, decodes each line via `streamEvent` (`stream.go:136`), emits `BackendEvent`s in `parseTurnLine` (`stream.go:199`). `readTurn` is already ctx-abortable (`stream.go:160-162`). Only the session-level `Close()` → `cmd.Process.Kill()` (`stream.go:441`) stops the subprocess today; there is no turn-level interrupt.
- **Daemon fan-out**: `sessionDriver.fanOut` (`core/host/turnloop.go:459`) is the `EventSink`/`RoutedEventSink.Emit` seam to the terminal gateway.
- **Cost flows**: `streamEvent.TotalCostUSD` → `BackendEvent{Kind:"result",Cost}` → `hub.go` closure → reply `Event.Cost` → `turnloop` record + fan-out → tui `formatCost`. **The token counter rides this exact path.**

## 4. Layer A — Visual & Ergonomic Redesign (Tier 1)

All within `plugins/terminal/tui`. No contract changes.

### 4.1 `theme.go`
Replace the Honkai palette with a Claude palette:
- Surface `#0f0f0e`, text `#d7d3ca`, dim `#6b6a63`, single warm accent `#d97757`, green `#7aa874`, red `#c9736a`, selection bg `#26261f`.
- Glyphs: `●` (tool call), `⎿` (tool result / continuation), `✳` (spinner busy), `>` (prompt), `❯` (menu selection cursor). Remove `glyphGutter`, `glyphBrand`, the `◆/◇/⟡/›` set, and the role-spine language.
- `spinFrames`: Claude-style rotating asterisk set (e.g. `✳ ✵ ✶ ✷ ✸ ✹`) advanced on the working tick.
- Remove `panelBorder` (no enclosing card), `paletteBorder` (inline menu instead), the tab-strip styles, and the violet/cyan role styles.

### 4.2 `render.go` + `tui.go View()`
- Drop the enclosing `panelBorder.Width(...)` wrap and the permanent `brandRow`/`tabStrip`.
- `renderEntry` maps roles to Claude shapes:
  - user → `> {text}` in dim (`#8a8880`), no gutter.
  - agent prose → bare text, full width, no gutter.
  - tool (`status`/`tool`) → `● {Tool}({Detail})`; a following result/continuation line as `  ⎿ {summary}` in dim. Tool name + detail already exist (`toolDetail`, `stream.go:118`).
  - cost → folded into the reply summary / spinner, not a standalone violet line.
- `View()` parts order becomes: transcript viewport → (permission menu | palette | shortcuts) → spinner/status row → input row → dim hint line. No brand row, no tab strip.

### 4.3 `palette.go` — `/` autocomplete
- Restyle from a bordered cyan box to an inline Claude menu: rows directly under the input, selected row prefixed `❯` in warm accent, description dim, no border.
- Command set: `/clear` (clear conversation), `/help` (shortcuts), `/session switch <name>`, `/session create --name`, `/resume <name>`. Keep existing `filterCommands`/`completePal` logic; only the render (`paletteView`) and the command list change.

### 4.4 New `@` file-mention (`mention.go`)
- When the input word under the cursor starts with `@`, open an inline completion list of paths from the **session's worktree directory** (the gateway knows the session's channel; the worktree path is available via the session/foreground record).
- Selecting a path replaces the `@partial` with `@relative/path`. The mention is inserted as **plain text** — the `claude` backend resolves `@` mentions itself. TUI does not read file contents.
- Reuses the palette's list-render + key-handling pattern (up/down/tab/enter/esc); shares the inline-menu style.

### 4.5 `?` shortcuts panel + input affordances
- Typing `?` on an empty input (or a bound key) toggles a dim shortcuts panel: `↑↓ history/scroll · ⏎ send · ⌥⏎ newline · esc interrupt · ctrl+v paste image · / commands · @ files`.
- `↑`/`↓` on an empty/single-line input recalls previous submitted prompts (input history ring, TUI-local).
- `⌥⏎` inserts a newline; `⏎` sends (already via textarea — confirm keymap).

### 4.6 Permission menu (Claude allow/deny)
- When the backend surfaces a `PendingChoice` (`ChoiceAware`, already wired through the pick path), render it as a Claude permission prompt:
  ```
  Herrscher wants to run Bash(rm -rf build/)
  ❯ 1. Yes
    2. Yes, and don't ask again for Bash
    3. No, tell Herrscher what to do differently  (esc)
  ```
- Selection emits the existing `pick` frame with the chosen value. `picker.go` supplies the list widget; only styling + the framing header change.

### 4.7 Invisible multi-session
- No permanent tab bar. One conversation on screen at a time.
- `/session switch` opens the existing `picker.go` as a session switcher listing sessions (label, unread dot, busy state). Selecting one swaps the active transcript.
- Unread/busy state on background sessions is preserved internally (`tab` struct) but only surfaced inside the switch picker, not as chrome.

## 5. Layer B — Live Token Counter (Tier 2)

The `usage` object is emitted by `claude` on every `assistant` line and on the terminal `result` line, and is currently dropped by the Go decoder.

### 5.1 `herrscher-claude-backend` (`stream.go`)
- Extend `streamEvent` with a `usage` field on the message and top-level:
  ```go
  Message struct {
      Content []contentBlock `json:"content"`
      Usage   *usage         `json:"usage"`
  } `json:"message"`
  Usage *usage `json:"usage"` // present on result lines
  ```
  with `type usage struct { InputTokens, OutputTokens, CacheReadInputTokens, CacheCreationInputTokens int }`.
- In `parseTurnLine`:
  - On `assistant` lines with usage, emit a live `BackendEvent{Kind:"usage", InTokens, OutTokens}` so the counter grows during the turn.
  - On the `result` line, fold the final cumulative usage into the terminal `BackendEvent{Kind:"result", Cost, InTokens, OutTokens}` and `turnResult`.
- Bump module version (minor).

### 5.2 `herrscher-contracts`
- `BackendEvent` (`backend.go:38`): add `InTokens int` and `OutTokens int` (documented as set on `Kind == "usage"` and `Kind == "result"`).
- `Event` (`event.go:16`): add `Tokens int json:"tokens,omitempty"` (documented: cumulative output tokens, carried live on `status`/`chunk` and finally on the terminal `reply`, like `Cost`). `omitempty` keeps the wire backward-compatible.
- Bump module version (minor). Nothing breaks: additive fields only.

### 5.3 `herrscher` (`core/bridge/hub.go`, `core/host/turnloop.go`)
- In `runOneTurn`'s `onEvent` closure (`hub.go:82`), capture tokens like cost: track running `inTok/outTok`; on `Kind=="usage"` update, on `Kind=="result"` set final.
- Emit the live token count on the intermediate events (`emitBackendEvent`, `hub.go:126`) — carry `Tokens` on the `chunk`/`status` Event so the spinner updates — and stamp the final `Tokens` on the reply Event (`hub.go:93`) alongside `Cost`.
- `turnloop.go` `fanOut`/`awaitTurn`: pass `Event.Tokens` through unchanged (it already fans whole Events).
- Bump both deps.

### 5.4 tui
- Spinner line renders `↑ {formatTokens(Tokens)}` (e.g. `3.4k`) between the elapsed timer and cost when `Tokens > 0`.
- Reply summary line may show final tokens + cost.

## 6. Layer C — `esc` to Interrupt (Tier 2)

Today there is no turn-level cancel; only session teardown kills the subprocess.

### 6.1 `herrscher-contracts`
- Define a new inbound frame type `interrupt` (an `Event` with `T:"interrupt"`), documented alongside `input`/`pick` in `event.go`. No new fields.

### 6.2 `herrscher` (`core/bridge/hub.go`)
- Accept the `interrupt` frame (`hub.go:62-67` inbound switch) and cancel the current turn's context. `readTurn` already returns `ctx.Err()` on cancel (`stream.go:160-162`); `runOneTurn` then emits the existing `abandoned` UI signal.
- Route it from the daemon (`turnloop.go`) the same way `input`/`pick` reach the bridge.

### 6.3 `herrscher-claude-backend` (`stream.go`)
Interrupt mechanism — resolved empirically during implementation, in this preference order:
1. **Primary (graceful):** write a stream-json control message to the subprocess stdin: `{"type":"control_request","request":{"subtype":"interrupt"}}`. If the installed `claude` honors it, the current turn stops without tearing down the session — no resume needed. Verify against the target `claude` version before committing to this path.
2. **Fallback (kill + auto-resume):** on ctx cancel, `Close()` the session (`cmd.Process.Kill()`, `stream.go:441`). The existing restart-with-`--resume` logic in `streamResponder.Respond` (`stream.go:382-397`) revives the session on the next `Send`, using the captured `sessID` (`stream.go:260`). **UX caveat (accepted):** the interrupted turn's partial work is discarded and the session resumes from the last `session_id` — this matches Claude's `esc` semantics.
- Bump module version.

### 6.4 tui
- While a turn is active, `esc` emits the `interrupt` frame (instead of, or in addition to, its current "close menu / clear input" behavior — `esc` interrupts only when a turn is in flight; otherwise it retains menu/clear behavior).
- Spinner hint reads `✳ Working… (esc to interrupt · {elapsed}s · ↑ {tokens} · ${cost})`.

## 7. Delivery Order (3 coordinated PRs)

1. **`herrscher-contracts`** — add `Event.Tokens`, `BackendEvent.InTokens/OutTokens`, the `interrupt` frame doc; minor version bump. Merges first (additive, nothing breaks).
2. **`herrscher-claude-backend`** — parse `usage`, emit token `BackendEvent`s, honor interrupt (primary control message with kill+resume fallback); bump contracts dep + own version.
3. **`herrscher`** — bump both deps; wire tokens through `hub.go`/`turnloop.go`; route the `interrupt` frame; **and** ship all of Layer A (visual + ergonomics) in the same PR.

## 8. Testing

- **Layer A (tui):** golden-render tests for `renderEntry` shapes (user `>`, agent prose, `●`/`⎿` tool lines), palette inline render, `@` mention completion (given a fake worktree listing), permission-menu render + `pick` emission, session-switch picker swap. Snapshot the `View()` composition (no card, no tab strip).
- **Layer B (tokens):** claude-backend `parseTurnLine` table test decoding real `assistant`+`result` lines with `usage`, asserting emitted `BackendEvent` token fields; hub closure test asserting final `Event.Tokens` on the reply; tui spinner formatting (`formatTokens`).
- **Layer C (interrupt):** bridge test that an `interrupt` frame cancels the turn ctx and yields an `abandoned` signal; claude-backend test for the chosen mechanism (control-message write, or Close+resume-on-next-Send with a fake io pair); tui test that `esc` emits `interrupt` only while a turn is active.
- Existing `stream.go` tests (io-pair injection) extend to cover usage + interrupt without a real subprocess.

## 9. Risks & Mitigations

- **3-repo version coordination.** Mitigate by ordering (contracts → backend → herrscher) and additive-only contract changes (`omitempty`), so intermediate states never break existing consumers (Discord gateway, other backends).
- **Interrupt mechanism uncertainty.** The primary control-message path depends on the installed `claude` supporting it; the kill+resume fallback is always available, so interrupt ships regardless. Decide empirically, document which path shipped.
- **Worktree path for `@` mentions.** If the session's worktree dir is not readily available to the gateway, `@` completion degrades to free-text (still inserted, claude still resolves it) — a graceful fallback, not a blocker.
- **Token semantics.** `Event.Tokens` is defined as cumulative output tokens for a single, consistent counter; input/cache tokens are available in `BackendEvent` if a richer display is wanted later (deferred, YAGNI).
