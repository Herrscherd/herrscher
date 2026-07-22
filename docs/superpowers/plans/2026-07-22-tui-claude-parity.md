# TUI Claude Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Herrscher's terminal TUI a faithful copy of the Claude Code interface (sober look + parity ergonomics), plus a live token counter and `esc`-to-interrupt.

**Architecture:** Three coordinated repos. `herrscher-contracts` adds additive wire fields (`Event.Tokens`, `BackendEvent.InTokens/OutTokens`) and an `interrupt` frame. `herrscher-claude-backend` parses `usage` from stream-json and honors interrupt. `herrscher` wires tokens through the bridge/hub, routes interrupt, and rewrites `plugins/terminal/tui` for the Claude look/feel.

**Tech Stack:** Go, Bubble Tea + lipgloss (TUI), stream-json (`claude -p`).

## Global Constraints

- Contract changes are **additive only** (`omitempty` / new zero-valued fields) — nothing existing may break (Discord gateway, codex/cursor backends).
- Delivery order is strict: **contracts → claude-backend → herrscher**. Each repo bumps a **minor** version.
- All TUI work stays in `plugins/terminal/tui`; no contract change for Layer A.
- Single warm accent `#d97757`; no enclosing card, no permanent tab bar (Claude look).
- TDD: failing test → implement → pass → commit. Tests use io-pair injection; no real `claude` subprocess.

---

## PR 1 — herrscher-contracts (repo `~/herrscher-work/herrscher-contracts`, branch `feat/tui-parity`)

### Task 1: Add token fields + interrupt frame

**Files:**
- Modify: `event.go` (`Event` struct ~L16-35; doc comment ~L3-15)
- Modify: `backend.go` (`BackendEvent` struct ~L38-44)

**Interfaces produced (consumed by PR2 + PR3):**
- `contracts.Event.Tokens int` (`json:"tokens,omitempty"`) — cumulative output tokens; carried live on `status`/`chunk`, finally on `reply`.
- `contracts.BackendEvent.InTokens int`, `.OutTokens int` — set on `Kind=="usage"` and `Kind=="result"`.
- New inbound frame value `Event{T:"interrupt"}` — documented alongside `input`/`pick`.

- [ ] **Step 1:** Add to `Event`: `Tokens int \`json:"tokens,omitempty"\`` with a doc line ("cumulative output tokens, live on status/chunk and final on reply, like Cost"). Add `interrupt` to the example block in the struct doc comment.
- [ ] **Step 2:** Add to `BackendEvent`: `InTokens int` and `OutTokens int` with a comment ("set on Kind == \"usage\" (live) and Kind == \"result\" (final)"). Extend the `Kind` comment to list `"usage"`.
- [ ] **Step 3:** `go build ./... && go vet ./...` — expect clean.
- [ ] **Step 4:** Commit: `feat(event): add Event.Tokens, BackendEvent token fields, interrupt frame`.
- [ ] **Step 5:** Tag minor bump (e.g. `v0.2.0`); push branch + tag. (Record the version for PR2/PR3 `go get`.)

---

## PR 2 — herrscher-claude-backend (repo `~/herrscher-work/herrscher-claude-backend`, branch `feat/tui-parity`)

### Task 2: Parse `usage` from stream-json

**Files:**
- Modify: `stream.go` (`streamEvent` ~L136-145; `parseTurnLine` ~L199-249; `turnResult` ~L99-105)
- Test: `stream_test.go` (parse table tests)

**Interfaces:**
- Consumes: `contracts.BackendEvent.InTokens/OutTokens` (Task 1).
- Produces: `parseTurnLine` emits `BackendEvent{Kind:"usage", InTokens, OutTokens}` on assistant lines carrying usage, and folds final usage into the terminal `BackendEvent{Kind:"result", ...}` + `turnResult`.

- [ ] **Step 1 (failing test):** Add `TestParseTurnLine_UsageEmitsTokens`: feed an `assistant` line with `"message":{"usage":{"input_tokens":10,"output_tokens":42},"content":[...]}`; assert a `usage` BackendEvent with `OutTokens==42` was emitted. Feed a `result` line with `usage` + `total_cost_usd`; assert the `result` BackendEvent carries `OutTokens` and `Cost`.
- [ ] **Step 2:** Run `go test ./... -run UsageEmitsTokens` — expect FAIL (no usage parsed).
- [ ] **Step 3:** Add `type usage struct { InputTokens int \`json:"input_tokens"\`; OutputTokens int \`json:"output_tokens"\`; CacheReadInputTokens int \`json:"cache_read_input_tokens"\`; CacheCreationInputTokens int \`json:"cache_creation_input_tokens"\` }`. Add `Usage *usage \`json:"usage"\`` inside the `Message` anon struct and a top-level `Usage *usage \`json:"usage"\`` on `streamEvent` (result lines put usage at top level). In `parseTurnLine` `assistant` branch: after the content loop, if `ev.Message.Usage != nil`, emit `BackendEvent{Kind:"usage", InTokens:u.InputTokens, OutTokens:u.OutputTokens}`. In `result` branch: pick `u := ev.Usage` (fallback `ev.Message.Usage`); set `InTokens/OutTokens` on the emitted result `BackendEvent` and add `InTokens/OutTokens` to `turnResult`.
- [ ] **Step 4:** Run the test — expect PASS. Then full `go test ./...`.
- [ ] **Step 5:** Commit: `feat(stream): parse usage tokens from stream-json`.

### Task 3: Honor turn interrupt

**Files:**
- Modify: `stream.go` (`streamSession` ~L255-261; `Send` ~L316; `streamResponder.Respond` ~L358-407; interrupt entry point)
- Test: `stream_test.go`

**Interfaces:**
- Consumes: turn ctx cancellation from the bridge (PR3 Task 5).
- Produces: an interrupt path — primary control-message write; fallback Close()+resume-on-next-Send.

- [ ] **Step 1 (spike, not committed):** With a throwaway harness, test whether the installed `claude` honors a stdin control message `{"type":"control_request","request":{"subtype":"interrupt"}}` mid-turn. Record the result in the commit body.
- [ ] **Step 2 (failing test):** Add `TestInterrupt_DropsAndResumes` using an injected io pair: start a turn, cancel its ctx, assert `readTurn` returns `ctx.Err()` and the session is dropped; assert the next `Send` triggers a restart carrying `--resume <sessID>`.
- [ ] **Step 3:** Run — expect FAIL where restart/resume assertion is unmet.
- [ ] **Step 4:** Implement the chosen mechanism. **Primary:** add `func (s *streamSession) Interrupt() error` that writes the control-message line to stdin (guarded by `mu`). **Fallback (always wired):** ensure `streamResponder.Respond` already drops the session on `ctx.Err()` (verify L374-380) and that the next `Send` restarts with `--resume` using `sessID` (verify L382-397). If primary is unsupported, `Interrupt()` calls `Close()`.
- [ ] **Step 5:** Run tests — expect PASS. Full `go test ./...`.
- [ ] **Step 6:** Commit: `feat(stream): turn-level interrupt (control-msg / kill+resume)`. Bump minor version; `go get herrscher-contracts@<v0.2.0>` first; push branch + tag.

---

## PR 3 — herrscher (repo `~/herrscher-work/herrscher`, branch `feat/tui-claude-parity`)

Bump deps first: `go get github.com/Herrscherd/herrscher-contracts@<v>` and `...herrscher-claude-backend@<v>`; `go mod tidy`. Commit `chore(deps): bump contracts + claude-backend`.

### Task 4: Wire tokens through bridge → reply Event

**Files:**
- Modify: `core/bridge/hub.go` (`runOneTurn` onEvent closure ~L82-93; `emitBackendEvent` ~L126-139)
- Test: `core/bridge/hub_test.go`

**Interfaces:**
- Consumes: `BackendEvent.InTokens/OutTokens` (PR2), `Event.Tokens` (PR1).
- Produces: live `Event.Tokens` on `chunk`/`status`; final `Event.Tokens` on the `reply`.

- [ ] **Step 1 (failing test):** `TestRunOneTurn_StampsTokens`: drive a fake backend emitting `usage` then `result` BackendEvents; assert the terminal reply Event has `Tokens == final OutTokens`.
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** In the onEvent closure track `outTok int`; on `Kind=="usage"` set `outTok = be.OutTokens`; on `Kind=="result"` set final. In `emitBackendEvent`, carry `Tokens: outTok` on the `chunk`/`status` Events. In `runOneTurn`'s reply Emit, add `Tokens: outTok` alongside `Cost`.
- [ ] **Step 4:** Run — PASS.
- [ ] **Step 5:** Commit: `feat(bridge): carry token counts on turn events`.

### Task 5: Route the interrupt frame daemon → bridge

**Files:**
- Modify: `core/bridge/hub.go` (inbound frame switch ~L62-67)
- Modify: `core/host/turnloop.go` (path that sends `input`/`pick` to the bridge)
- Test: `core/bridge/hub_test.go`

**Interfaces:**
- Consumes: `Event{T:"interrupt"}` (PR1); `streamSession.Interrupt()` / ctx cancel (PR2).
- Produces: an interrupt cancels the in-flight turn ctx; the existing `abandoned` UI signal fires.

- [ ] **Step 1 (failing test):** `TestInbound_InterruptCancelsTurn`: start a turn, deliver `Event{T:"interrupt"}`, assert the turn ctx is cancelled and an `abandoned` event is emitted.
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Add an `interrupt` case to the inbound switch that cancels the current turn's `context.CancelFunc`; ensure `turnloop.go` forwards an `interrupt` Event down the same channel as `input`/`pick`.
- [ ] **Step 4:** Run — PASS.
- [ ] **Step 5:** Commit: `feat(bridge): route interrupt frame to cancel the active turn`.

### Task 6: theme.go — Claude palette + glyphs

**Files:** Modify `plugins/terminal/tui/theme.go` (whole file). Test: golden via Task 7.

- [ ] **Step 1:** Replace palette: surface `#0f0f0e`, text `#d7d3ca`, dim `#6b6a63`, accent `#d97757`, green `#7aa874`, red `#c9736a`, selection bg `#26261f`. Glyphs: tool `●`, result `⎿`, spinner-set `{"✳","✵","✶","✷","✸","✹"}`, prompt `>`, menu cursor `❯`. Remove `glyphGutter/glyphBrand/glyphAgent/glyphYou/glyphThinking/glyphBusy`, the gutter/spine styles, `panelBorder`, `paletteBorder`, tab-strip styles, violet/cyan role styles. Keep a `dimStyle`, `warmStyle`, `userStyle`, `greenStyle`, `selStyle`, `spinnerStyle`.
- [ ] **Step 2:** `go build ./plugins/terminal/...` — fix references broken by removed symbols (compiler drives the rename; each broken site is handled in its owning task below).
- [ ] **Step 3:** Commit: `feat(tui): Claude palette and glyph set`.

### Task 7: render.go + View() — full-width Claude flow

**Files:** Modify `plugins/terminal/tui/render.go`, `plugins/terminal/tui/tui.go` (`View()` ~L1136, `renderInto`/`renderEntry`). Test: `plugins/terminal/tui/render_test.go`.

- [ ] **Step 1 (failing test):** golden tests: user entry renders `> {text}` in dim (no gutter); agent prose renders bare full-width; a tool entry renders `● {Tool}({Detail})` and its result as `  ⎿ {summary}`.
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Rewrite `renderEntry`/`block` to the shapes above (drop the colored spine `block`). In `View()` remove the `panelBorder.Width(...)` wrap, `brandRow()`, and `tabStrip()`; part order = transcript → (menu|palette|shortcuts) → status/spinner → input → hint.
- [ ] **Step 4:** Run — PASS; visually confirm `go run` build.
- [ ] **Step 5:** Commit: `feat(tui): full-width Claude message flow, ●/⎿ tool lines`.

### Task 8: palette.go — inline `/` menu + command set

**Files:** Modify `plugins/terminal/tui/palette.go`. Test: `plugins/terminal/tui/palette_test.go`.

- [ ] **Step 1 (failing test):** assert `paletteView()` renders selected row prefixed `❯` in accent with no border box, and the command set includes `/clear`, `/help`, `/session switch`, `/session create`, `/resume`.
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Rewrite `paletteView()` to inline rows (no `paletteBorder`); update the `m.cmds` seed list to the Claude command set.
- [ ] **Step 4:** Run — PASS.
- [ ] **Step 5:** Commit: `feat(tui): inline slash-command autocomplete, Claude command set`.

### Task 9: `@` file-mention autocomplete

**Files:** Create `plugins/terminal/tui/mention.go` (+ `mention_test.go`); wire key handling in `tui.go`.

- [ ] **Step 1 (failing test):** `TestMentionComplete`: given a fake worktree listing and input `foo @sr`, completing yields `foo @src/` (or the single match) inserted as plain text.
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Implement `mentionOpen(input, cursor) bool`, `mentionMatches(dir, prefix) []string`, `completeMention(...)`; open an inline list (reuse palette style) when the cursor word starts with `@`; read paths from the session worktree dir (from the active `tab`/session record); insert `@relative/path` as text.
- [ ] **Step 4:** Run — PASS.
- [ ] **Step 5:** Commit: `feat(tui): @ file-mention autocomplete from session worktree`.

### Task 10: `?` shortcuts panel + input history

**Files:** Modify `plugins/terminal/tui/tui.go` (key handling, view part). Test: `tui_test.go`.

- [ ] **Step 1 (failing test):** `?` on empty input toggles a shortcuts panel string containing `esc interrupt`, `⌥⏎ newline`, `/ commands`, `@ files`; `↑` on empty input recalls the last submitted prompt.
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Add a `showShortcuts bool` + a history ring of submitted prompts; render the panel as a dim block; bind `?`/`↑`/`↓`.
- [ ] **Step 4:** Run — PASS.
- [ ] **Step 5:** Commit: `feat(tui): shortcuts panel and input history`.

### Task 11: Claude-style permission menu

**Files:** Modify `plugins/terminal/tui/picker.go`, `tui.go`. Test: `picker_test.go`.

- [ ] **Step 1 (failing test):** given a `PendingChoice{Question:"run Bash(rm -rf build/)", Options:[Yes; Yes don't ask again; No]}`, the render shows the framing header + `❯ 1. Yes` / `  2. …` / `  3. …  (esc)`; selecting row 1 emits a `pick` with its value.
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Restyle the picker render to the Claude permission shape; keep the existing `pick`-emission logic.
- [ ] **Step 4:** Run — PASS.
- [ ] **Step 5:** Commit: `feat(tui): Claude-style allow/deny permission menu`.

### Task 12: Invisible multi-session switch + spinner (tokens/esc/cost)

**Files:** Modify `plugins/terminal/tui/tui.go` (spinner/status row, `/session switch` wiring), `picker.go`. Test: `tui_test.go`.

- [ ] **Step 1 (failing test):** spinner line while a turn is active renders `✳ …(esc to interrupt · {n}s · ↑ {tokens} · ${cost})` when `Tokens>0`; `/session switch` opens the picker listing sessions and swaps the active transcript on select; `esc` during an active turn emits `Event{T:"interrupt"}`.
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Add `formatTokens(int) string` (`3.4k`); render the spinner hint; ensure no permanent tab strip; wire `/session switch` to the picker; make `esc` emit `interrupt` only while a turn is in flight (else keep menu/clear behavior).
- [ ] **Step 4:** Run — PASS; full `go test ./...`; `bun`/`go` build.
- [ ] **Step 5:** Commit: `feat(tui): token+esc+cost spinner, invisible session switch`.

---

## Self-Review

- **Spec coverage:** §4 Layer A → Tasks 6-12; §5 tokens → Tasks 1,2,4,12; §6 interrupt → Tasks 1,3,5,12; §7 delivery order → PR structure. All covered.
- **Type consistency:** `Event.Tokens` (Task1) used in Tasks 4,12; `BackendEvent.InTokens/OutTokens` (Task1) used in Tasks 2,4; `streamSession.Interrupt()` (Task3) referenced by Task5 via ctx cancel; `formatTokens` defined + used in Task12.
- **Placeholders:** interrupt mechanism is empirically resolved in Task3 Step1 with a guaranteed fallback — not a placeholder.
