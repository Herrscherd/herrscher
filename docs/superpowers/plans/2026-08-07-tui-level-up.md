# TUI level-up implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Take the terminal TUI past plain text — images on every terminal and in
every format, links that can be opened, rich blocks in the transcript, and a
scrollback that can be searched.

**Architecture:** One capability probe runs at startup and is read by every
feature; each feature picks its best available rendering from it and never
errors. Four phases follow the foundation, each shippable alone: images, links,
rich blocks, navigation.

**Tech Stack:** Go, Bubble Tea / lipgloss / glamour, `image` and
`golang.org/x/image` for decoding, the kitty graphics protocol already in
`tui/image.go`, sixel and unicode half-blocks as new encoders.

## Global Constraints

- Nothing in this work may reach `core/`. It lives in `plugins/terminal/tui`.
- No test opens a network connection, launches a browser, or requires a
  particular terminal. The probe reads an injected environment map; the launcher
  is an injected function.
- Every failure degrades to a plainer rendering and keeps the turn. No failure
  in this document may drop a message or end a turn.
- Nothing opens on its own. Only an operator gesture opens a link or an editor.
- Remote images are fetched only from hosts on the existing gateway-supplied
  attachment allowlist.
- Follow the density of the surrounding package: doc comments say why, and the
  existing `_test.go` naming and frame-assertion style are the pattern to match.
- `go build ./... && go vet ./... && go test ./...` passes at the end of every task.

---

### Task 1: The capability probe

**Files:**
- Create: `plugins/terminal/tui/capabilities.go`
- Test: `plugins/terminal/tui/capabilities_test.go`

**Interfaces:**
- Produces:
  - `type Graphics int` — `GraphicsNone`, `GraphicsSixel`, `GraphicsKitty`.
  - `type Colour int` — `Colour16`, `Colour256`, `ColourTrue`.
  - `type Capabilities struct { Graphics Graphics; Hyperlinks, Mouse bool; Colour Colour; Terminal string }`
  - `func Probe(env func(string) string) Capabilities` — pure in its input.

**Detection:** `KITTY_WINDOW_ID` or `TERM` containing `kitty`, or
`TERM_PROGRAM` in {`ghostty`, `WezTerm`} → `GraphicsKitty`. `TERM` containing
`sixel`, or `TERM_PROGRAM` = `mlterm` → `GraphicsSixel`. Otherwise
`GraphicsNone`. `COLORTERM` in {`truecolor`, `24bit`} → `ColourTrue`; `TERM`
containing `256color` → `Colour256`; else `Colour16`. Hyperlinks true for every
terminal in the kitty/ghostty/wezterm/iTerm set. `Terminal` is the best
human-readable name found, for the diagnostic screen.

The existing `supportsKitty` in `tui/image.go` is replaced by a read of this
value — one detection, not two.

- [ ] **Step 1: Write the failing test** — one table row per terminal
      (kitty, ghostty, wezterm, iTerm2, Terminal.app, xterm-256color, plain
      `TERM=dumb`, empty environment), asserting all four fields.
- [ ] **Step 2: Run it, confirm it fails** — `go test ./plugins/terminal/tui/ -run TestProbe -v`
- [ ] **Step 3: Implement `capabilities.go`, and repoint `supportsKitty`**
- [ ] **Step 4: Run the package suite** — every existing image test still passes.
- [ ] **Step 5: Commit** — `feat(tui): ask the terminal what it can do, once`

---

### Task 2: The diagnostic screen

**Files:**
- Create: `plugins/terminal/tui/diagnostics.go`
- Test: `plugins/terminal/tui/diagnostics_test.go`
- Modify: `plugins/terminal/tui/palette.go`

**Interfaces:**
- Consumes: `Capabilities` from Task 1.
- Produces: the screen model, and a palette entry that opens it.

**Content:** one row per capability — name, resolved value, and the feature it
enables — plus a short line naming which terminals get everything and what a
plain terminal falls back to.

- [ ] **Step 1: Write the failing test** — render the screen from a fixed
      `Capabilities` and assert each resolved value and its feature line appear;
      a second case with `GraphicsNone` asserts the fallback is named rather
      than the row being hidden.
- [ ] **Step 2: Run it, confirm it fails**
- [ ] **Step 3: Implement**
- [ ] **Step 4: Run the package suite**
- [ ] **Step 5: Commit** — `feat(tui): say which capabilities are live and why`

---

### Task 3: Decode every format, and move the size cap

**Files:**
- Create: `plugins/terminal/tui/image_decode.go`
- Test: `plugins/terminal/tui/image_decode_test.go`
- Modify: `plugins/terminal/tui/image.go`

**Interfaces:**
- Produces:
  - `func decodeImage(data []byte) (image.Image, error)` — PNG, JPEG, GIF, WebP.
  - `func downscale(img image.Image, maxRows int) image.Image` — bounded by the
    existing `previewRows`.
  - `func encodePNG(img image.Image) ([]byte, error)` — what the kitty encoder
    consumes.

**The cap moves:** `maxPreviewBytes` (512 KiB) now applies to the *encoded*
payload after downscaling. A separate, generous source bound
(`maxDecodeBytes`, 8 MiB) keeps decoding cheap. Both are named constants with
comments saying what each protects.

- [ ] **Step 1: Write the failing test** — fixtures generated in the test (a
      small PNG, JPEG and GIF encoded in-memory at init, no binary files
      committed): each decodes; an oversized source is refused by
      `maxDecodeBytes`; a large image downscales to under `maxPreviewBytes`
      where the old source-side cap would have rejected it; corrupt bytes return
      an error rather than panicking.
- [ ] **Step 2: Run it, confirm it fails**
- [ ] **Step 3: Implement, and repoint `previewEscapes` at the new pipeline**
- [ ] **Step 4: Run the package suite** — the existing PNG tests must still pass unchanged.
- [ ] **Step 5: Commit** — `feat(tui): every image format, capped where it counts`

---

### Task 4: Sixel and unicode-block encoders

**Files:**
- Create: `plugins/terminal/tui/image_sixel.go`, `plugins/terminal/tui/image_blocks.go`
- Test: `plugins/terminal/tui/image_sixel_test.go`, `plugins/terminal/tui/image_blocks_test.go`
- Modify: `plugins/terminal/tui/image.go` — pick the encoder from `Capabilities`.

**Interfaces:**
- Consumes: `decodeImage`, `downscale`, `Capabilities`.
- Produces:
  - `func sixelEscape(img image.Image) string`
  - `func blockRender(img image.Image, cols int) string` — half-block characters
    with foreground/background colour, two pixel rows per text row.

- [ ] **Step 1: Write the failing tests** — a 2×2 known-colour image renders to
      the expected sixel introducer and payload shape; the same image renders to
      one row of half-blocks with the expected two colours; a `GraphicsNone`
      capability selects `blockRender`, `GraphicsSixel` selects `sixelEscape`,
      `GraphicsKitty` selects the existing kitty path.
- [ ] **Step 2: Run them, confirm they fail**
- [ ] **Step 3: Implement**
- [ ] **Step 4: Run the package suite**
- [ ] **Step 5: Commit** — `feat(tui): show the picture even without a graphics protocol`

---

### Task 5: Remote images, under the allowlist

**Files:**
- Create: `plugins/terminal/tui/image_fetch.go`
- Test: `plugins/terminal/tui/image_fetch_test.go`

**Interfaces:**
- Consumes: `decodeImage`.
- Produces: `func fetchImage(ctx context.Context, get func(context.Context, string) ([]byte, error), allow []string, rawURL string) ([]byte, error)`
  — the fetcher is injected, so no test opens a socket.

**Behaviour:** the URL's host must match the allowlist; a refused host returns an
error naming the host and the fetcher is never called. A fetch failure returns
an error — the caller keeps the URL as a link and loses nothing.

- [ ] **Step 1: Write the failing test** — an allowed host calls the fetcher
      once; a refused host names the host and leaves a call counter at zero; an
      unparseable URL errors; a fetcher error propagates.
- [ ] **Step 2: Run it, confirm it fails**
- [ ] **Step 3: Implement, and wire the transcript's image URLs to it**
- [ ] **Step 4: Run the package suite**
- [ ] **Step 5: Commit** — `feat(tui): render an image posted as a link`

---

### Task 6: Link detection and selection

**Files:**
- Create: `plugins/terminal/tui/links.go`
- Test: `plugins/terminal/tui/links_test.go`
- Modify: `plugins/terminal/tui/render.go`

**Interfaces:**
- Consumes: `Capabilities` (for OSC 8).
- Produces:
  - `type Link struct { Label, Target string; Line, Start, End int; Kind LinkKind }`
    with `LinkURL` and `LinkFile`.
  - `func findLinks(lines []string) []Link` — bare URLs, markdown links, and
    `path/file.go:42` references.
  - `func renderLink(l Link, caps Capabilities, selected bool) string` — OSC 8
    when available, styled text otherwise; the selected one is highlighted.

- [ ] **Step 1: Write the failing test** — a table of transcript lines to
      expected links, including: a bare URL; a markdown link whose label differs
      from its target (asserting `Label` and `Target` are both captured and
      distinct); a `file.go:42` reference; a URL inside a code block, which is
      not a link; trailing punctuation excluded from the target.
- [ ] **Step 2: Run it, confirm it fails**
- [ ] **Step 3: Implement, and render links in the transcript**
- [ ] **Step 4: Run the package suite**
- [ ] **Step 5: Commit** — `feat(tui): find the links a transcript already contains`

---

### Task 7: Opening, by gesture only

**Files:**
- Create: `plugins/terminal/tui/open.go`
- Test: `plugins/terminal/tui/open_test.go`
- Modify: `plugins/terminal/tui/tui.go` — key bindings, status line.

**Interfaces:**
- Consumes: `Link`.
- Produces:
  - `type launcher func(target string) error`
  - `func systemLauncher() launcher` — `xdg-open` / `open` / `rundll32` by GOOS.
  - `func editorLauncher(env func(string) string) launcher` — `$EDITOR +42 file`.
  - `func openLink(l Link, sys, edit launcher) error` — `LinkFile` goes to the
    editor, everything else to the system handler.

**Status line:** whenever a link is selected, the status line shows the
**resolved target**, never the label. A markdown label can name one destination
and point at another; the operator's gesture must be informed. This is display,
not a confirmation.

- [ ] **Step 1: Write the failing test** —
      1. the open key on a selected link calls the injected launcher once with
         the resolved target;
      2. a message arriving, and a turn completing, never call it (counter stays
         zero) — this is the load-bearing assertion of this task;
      3. a `LinkFile` goes to the editor launcher with the line number, not to
         the system one;
      4. a launcher error surfaces in the status line and the turn survives;
      5. selecting a markdown link whose label lies renders the target in the
         status line, not the label.
- [ ] **Step 2: Run it, confirm it fails**
- [ ] **Step 3: Implement**
- [ ] **Step 4: Run the package suite**
- [ ] **Step 5: Commit** — `feat(tui): open a link, and only when asked`

---

### Task 8: Rich blocks

**Files:**
- Modify: `plugins/terminal/tui/render.go`, `plugins/terminal/tui/markdown.go`
- Create: `plugins/terminal/tui/blocks_rich.go`
- Test: `plugins/terminal/tui/blocks_rich_test.go`

**Interfaces:**
- Produces: `func renderDiff(s string, caps Capabilities) string`,
  `func renderTable(rows [][]string, width int) string`, and a foldable
  code-block model with a copy action.

- [ ] **Step 1: Write the failing test** — a unified diff colours additions and
      deletions per hunk and leaves context uncoloured; a table fits its width
      and truncates the widest cell rather than wrapping the row; a folded code
      block renders one summary line and expands on the fold key; copy puts the
      block's raw text on the clipboard seam already in `tui/clipboard.go`.
- [ ] **Step 2: Run it, confirm it fails**
- [ ] **Step 3: Implement**
- [ ] **Step 4: Run the package suite**
- [ ] **Step 5: Commit** — `feat(tui): make tool output readable`

---

### Task 9: Search and turn navigation

**Files:**
- Create: `plugins/terminal/tui/search.go`
- Test: `plugins/terminal/tui/search_test.go`
- Modify: the scrollback in `plugins/terminal/tui/tui.go`

**Interfaces:**
- Produces: a search overlay (open, type, next/previous, close) and
  `func turnBoundaries(lines []string) []int` driving jump-to-turn and folding.

- [ ] **Step 1: Write the failing test** — a query highlights every match and
      scrolls to the first; next/previous cycle and wrap; closing restores the
      prior scroll position; jump-to-turn lands on a boundary; folding an old
      turn collapses it to one line and unfolding restores it exactly.
- [ ] **Step 2: Run it, confirm it fails**
- [ ] **Step 3: Implement**
- [ ] **Step 4: Run the package suite**
- [ ] **Step 5: Commit** — `feat(tui): find something in a long conversation`

---

### Task 10: Documentation

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update the terminal section** — capabilities and their
      fallbacks, the diagnostic screen, the link gesture and why nothing opens
      by itself, and the new keys.
- [ ] **Step 2: Run the suite**
- [ ] **Step 3: Commit** — `docs: what the TUI does on which terminal`

The docs-site changes (`plugins/terminal.svx`, EN and FR) are a separate PR in
`herrscher-docs`, not part of this branch.
