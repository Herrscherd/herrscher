# TUI Redesign + Multimodal Attachments — Design

Date: 2026-07-21
Branch: `feat/tui-redesign`
Status: approved-in-principle (pending spec review)

## Problem

The terminal TUI (`plugins/terminal/tui/`, Bubble Tea) is visually rough and functionally thin:

1. **Single-line input.** The prompt is a `bubbles/textinput` (`tui.go:186`) — no multi-line
   editing, Enter always submits, no wrap. This is the top pain point.
2. **No word-wrap.** The transcript stores pre-rendered strings (`tab.lines`) joined with `\n`
   (`thinkingContent`, `tui.go:523`) and dumped into the viewport. Long lines are cut by the
   terminal; glyph-width-unaware, so emoji/CJK break alignment.
3. **No attachments.** `Terminal.Submit` (`terminal.go:126`) builds a `Message` with only
   `Content`. The whole media pipeline is dormant end-to-end (see below). Pasting an image is
   impossible.
4. **Generic look.** The brand is Honkai/sci-fi (memory: `herrscher-honkai-da`) but the render is
   flat rows, no message blocks, minimal accent.

## Media pipeline: current reality

- `contracts.Message.Attachments []Attachment` — exists (inbound shape, URL/filename).
- `contracts.Prompt.Attachments []string` — exists ("local filesystem paths to any attachments
  already downloaded for the backend to reference"). The model backend (private plugin) already
  consumes these into image blocks.
- **Gap:** `contracts.Event` is text-only (`Text string`). `turnloop.go:243` builds the input
  event from `m.Content` alone, dropping `m.Attachments`. `downloadImages` (`core/bridge/
  attachments.go`) has no live caller and only handles https CDN URLs. So attachments reach the
  backend nowhere today.

## Decisions (from brainstorming)

- Scope: all four axes — multi-line composer, word-wrap, attachments, Honkai visual overhaul.
- Image display: **chips + inline when supported** (kitty/iTerm2). Runtime is kitty
  (`TERM=xterm-kitty`). No sixel.
- Look: **Honkai / sci-fi affirmé**.
- Paste: **Ctrl+V pastes a clipboard image** (Wayland `wl-paste`; falls back to text paste when no
  image present).
- Media reaching the agent: **Option B — real multimodal.** Wire the dormant pipeline: paths flow
  `Message.Attachments → Event.Attachments → Prompt.Attachments → backend image blocks`.

## Architecture

### Component 1 — Structured transcript model (foundation, tui pkg)

Replace `tab.lines []string` with `tab.entries []entry`:

```go
type entry struct {
    role        string        // "you" | "agent" | "status" | "cost" | "scrollback"
    text        string        // logical (unwrapped, unstyled) body
    attachments []attachment  // chips rendered under the body
    streaming   bool          // agent entry still being appended to
}
type attachment struct { name, path, mime string; size int64 }
```

- `renderInto` appends/updates entries instead of pre-rendered strings.
- New `renderTranscript(width)` renders entries → wrapped, styled lines on demand:
  word-wrap via `reflow/wordwrap` + glyph-width via `runewidth` (already an indirect dep of
  lipgloss). Called from `syncViewport`; re-runs on `WindowSizeMsg` so resize re-wraps cleanly.
- `maxTabLines` becomes a cap on `entries`.
- Chunk streaming: consecutive `chunk` events append to the current agent entry's `text` (so the
  final wrap is over the whole message, not per-token fragments).

### Component 2 — Multi-line composer (tui pkg)

- Swap `textinput.Model` → `bubbles/textarea` (same `bubbles` module, no go.mod change).
- Key policy: **Enter = submit**, **Alt+Enter / Ctrl+J = newline**. (Shift+Enter is not reliably
  distinguishable in terminals.)
- Grows upward to a cap (8 rows) then scrolls internally.
- `chromeHeight` (`tui.go:150`) becomes dynamic on composer height, like `paletteHeight`.
- Palette (`/`) still triggers on a leading `/` on the first line.

### Component 3 — Attachments capture + display (tui pkg + terminal gateway)

Capture:
- `Ctrl+V`: run `wl-paste --list-types`; if an `image/*` type is present, `wl-paste --type
  image/png > <tempfile>`, add a pending `attachment`. Otherwise fall through to normal text paste.
- `/attach <path>`: add a local file as a pending attachment (validated: exists, is a regular
  file, size ≤ 10 MiB).
- Pending attachments render as chips on a row directly above the composer; `Ctrl+U`... (see
  Open questions) removes the last one.

Temp files: written under `os.TempDir()/herrscher-attachments/terminal/` (mirrors
`attachmentDir`). Named `paste-<n>.png` / sanitized basename.

Display:
- On submit, the user echo entry carries its `attachments`; chips render `[📎 name · 12KB]`.
- If terminal supports kitty graphics (detected via `$TERM`/`$TERM_PROGRAM`), an inline preview is
  emitted under the chip via the kitty graphics protocol (isolated in `image.go`). Fallback: chip
  only. Bounded preview height (e.g. ≤ 10 rows).

Send:
- `Backend.Submit(channel, text string)` → `Submit(channel string, text string, attachments
  []Attachment)` (TUI's own seam). `Terminal.Submit` populates `Message.Attachments` with
  `{Filename, ContentType, Size}` and a **local path** carried for the resolver (see Component 4).

### Component 4 — Multimodal wiring (contracts + core/host + core/bridge)

- **contracts (vX.Y):** add `Attachments []string` to `Event` (JSON `attachments,omitempty`).
  Additive, backward compatible; a decoder on the old struct simply ignores it. Bump published so
  the host's public build stays green (memory: `host-deps-must-be-public`).
- **Local path carriage:** the terminal writes pasted images to local temp files, so no download is
  needed. The gateway needs a way to hand the host a *local path* for an attachment. Chosen path:
  the terminal sets `Attachment.URL = "file://<abspath>"`; the host's resolver treats `file://` as
  an already-local attachment (no fetch, no CDN allowlist) and yields the path directly.
- **core/bridge/attachments.go:** add a `resolveAttachments(m)` that returns local paths — passing
  through `file://` paths as-is and (optionally, later) invoking `downloadImages` for https CDN
  URLs. Wire the SSRF/size guards to apply only to the https branch.
- **core/host/turnloop.go:** `poll` resolves `m.Attachments` → local paths and carries them on
  `Event{T:"input", Attachments: paths}`. The turn assembly passes them into
  `Prompt.Attachments`. The `human` fan-out event may also carry them for other EventSinks.

### Component 5 — Honkai visual overhaul (theme.go + render)

- Message blocks: per-role left gutter bar (accent color), role label, wrapped body indented under
  the gutter.
- Accent palette pushed harder (existing cyan `#56e1cd` / violet `#b18cff` kept as signature;
  add a Herrscher accent for gutters/headers). Keep the rounded `panelBorder`.
- Chips styled as pill tokens. Cost line as a faint right-aligned tag.
- All within existing lipgloss usage; no new deps.

## Phasing (implementation order)

1. **Foundation:** structured transcript + word-wrap + block rendering (Components 1, 5). No
   contracts change; fully testable in the tui package.
2. **Composer:** textarea + dynamic chrome (Component 2).
3. **Attachments capture + display:** paste/`/attach`, chips, kitty preview, `Terminal.Submit`
   signature (Component 3). Still no contracts change — chips render locally.
4. **Multimodal wiring:** contracts `Event.Attachments`, resolver `file://`, turnloop threading
   (Component 4). This is the cross-repo phase; gated on publishing the contracts bump.

Each phase is independently shippable; Phase 4 is the only one that touches the contracts repo.

## Testing

- Component 1: table tests for `renderTranscript` — wrap at width, wide-glyph width, chunk
  coalescing, entry cap trimming.
- Component 2: key-policy tests (Enter submits, Alt+Enter inserts newline), chrome-height math.
- Component 3: `wl-paste` capture behind an injectable clipboard reader (interface, faked in
  tests); temp-file creation + sanitization; chip rendering.
- Component 4: `resolveAttachments` (`file://` pass-through, https guarded); turnloop carries paths
  into `Prompt.Attachments` (extend existing turnloop tests).
- kitty `image.go`: pure encoder tested on bytes; detection stubbed.

## Open questions

- Remove-attachment keybinding (`Ctrl+U` vs a dedicated key) — pick during Phase 3.
- Whether the `human` fan-out event should also carry attachments (affects Discord echo) — defer;
  terminal-only for now.
- Preview height cap and whether to preview on both send and agent-returned images (agent-returned
  requires Event media both ways) — Phase 4 follow-up.
