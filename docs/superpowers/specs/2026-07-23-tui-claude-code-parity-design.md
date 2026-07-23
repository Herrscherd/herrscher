# TUI Claude-Code parity — composer & history

**Goal:** Make the terminal composer and transcript behave like Claude Code: long input auto-wraps and the box grows (never clips), and history renders cleanly at any width.

## Findings

The current source (v0.1.32) already implements the intended behaviour, verified empirically:

- **Composer wrap/grow/cap** — a long unbroken line wraps and the composer grows to `maxComposerLines` (8), then scrolls internally with the cursor tail visible (`displayRows`/`composerWrap` in `composer_height.go`).
- **Transcript wrap + auto-scroll** — long agent lines fold via `lipgloss.Width` and the viewport follows the bottom (`render.go`, `syncViewport`).

So the *primary* reason the operator sees breakage is a **stale installed binary** (`~/go/bin/herrscher` was `0.1.26`, source is `v0.1.32`).

## The one real defect

`Update`'s `tea.WindowSizeMsg` case sets the composer width (`SetWidth`) but never re-measures its height (`resizeComposer`). Narrowing the terminal re-wraps the draft to more rows while the composer keeps its old height, so the draft is clipped until the next keystroke. Fix: call `resizeComposer()` right after `SetWidth` in the resize handler.

## Scope (YAGNI)

No markdown rendering, no colour redesign, no new features — just the resize fix plus reinstalling the current binary so the operator runs the already-correct code.

## Tests

- `TestComposerRemeasuresOnResize` — a line that is 1 row at width 100 wraps to ≥2 rows after narrowing to width 40.
- Existing composer/transcript tests continue to pass.
