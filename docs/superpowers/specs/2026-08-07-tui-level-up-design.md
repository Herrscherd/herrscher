# TUI level-up design

## The problem

The terminal TUI renders markdown, runs a palette and a picker, and shows inline
images — but only PNG files, only attached locally, only under 512 KiB, and only
on terminals speaking the kitty graphics protocol. Everywhere else it shows
nothing and says nothing. URLs in the transcript are inert text. Tool output,
diffs and tables arrive as flat text. A long conversation cannot be searched.

## What this builds

One shared foundation, then four phases, each shippable on its own.

## Foundation: a capability probe

Every feature below depends on what the terminal can do, and today each one
would answer that question its own way. One probe runs at startup and produces a
value the rest of the package reads:

- **graphics** — kitty, sixel, or none
- **hyperlinks** — OSC 8 or not
- **mouse** — available or not
- **colour** — truecolor, 256, or 16

Detection is by terminal identity and environment (`TERM`, `TERM_PROGRAM`,
`KITTY_WINDOW_ID`, `COLORTERM`) plus, for graphics, the existing kitty check.
Probing is done once; nothing re-probes mid-session.

A **diagnostic screen**, opened from the palette, shows each capability, what it
was resolved to, and which features it enables. Without it, "the image does not
show up" is undiagnosable from the outside — the operator cannot tell a missing
protocol from a bug.

Degradation is silent at the point of use: a feature reads the probe and picks
its best available rendering. It never errors, and it never prints a notice into
the transcript.

### Phase 1 — Images everywhere, for everyone

- **All formats.** JPEG, GIF, WebP and PNG are decoded and re-encoded to what
  the active protocol wants, instead of PNG passing and everything else being
  dropped.
- **Remote images.** Images referenced by URL in the transcript are fetched and
  rendered, subject to the same host allowlist the host already applies to
  attachment downloads. An image from a host outside it is named, not fetched —
  the allowlist is the existing answer to "who may this process talk to", and
  this feature does not get its own.
- **Fallbacks.** kitty when present, sixel next, half-block unicode last. The
  unicode path always works, so a terminal with no graphics protocol still sees
  the picture, coarsely, rather than a blank.
- **The size cap moves.** Today 512 KiB is measured on the source file, which
  rejects a large photo that would downscale to a few kilobytes. The cap applies
  to the encoded payload after downscaling; the source is bounded separately and
  far more generously, to keep decoding cheap.

### Phase 2 — Live links

- URLs and `path/file.go:42` references in the transcript are detected and
  rendered as links: OSC 8 where available, styled text where not.
- Links are selectable from the keyboard and openable by an operator gesture.
  **Nothing ever opens on its own** — not on a message arriving, not on a turn
  completing. The gesture is the whole gate.
- Any scheme is handed to the system handler. The gate is the deliberate
  gesture, on the operator's own machine.
- Because a markdown label can name one destination and point at another, the
  **resolved target is shown in the status line** whenever a link is selected.
  This is display, not a confirmation prompt: the operator sees where the gesture
  goes before making it.
- A `file.go:42` reference opens the editor at the line, via `$EDITOR`.

### Phase 3 — Rich blocks in the transcript

- Tables rendered as tables.
- Diffs coloured per hunk.
- Code blocks foldable and copyable.
- Tool output rendered per tool rather than as flat text.

### Phase 4 — Navigation and search

- Search within the scrollback, with match highlighting and next/previous.
- Jump turn to turn.
- Fold older turns.

## File structure

New:

- `tui/capabilities.go` — the probe and its resolved value.
- `tui/diagnostics.go` — the capability screen.
- `tui/image_decode.go` — format decoding and downscaling.
- `tui/image_sixel.go`, `tui/image_blocks.go` — the two new encoders.
- `tui/image_fetch.go` — remote fetching under the host allowlist.
- `tui/links.go` — detection of URLs and file references, selection state.
- `tui/open.go` — the gesture handler and the system/editor invocation.
- `tui/search.go` — scrollback search and turn navigation.

Modified:

- `tui/image.go` keeps the kitty encoder and loses the format and source
  decisions to the files above — it is currently doing three jobs.
- `tui/render.go` and `tui/markdown.go` gain the rich blocks.
- The scrollback gains folding and the search overlay.

## Errors

Every failure in this document degrades to the plainer rendering and keeps the
turn. An undecodable image leaves its chip; an unreachable remote image leaves
its URL as a link; a terminal without OSC 8 leaves styled text that can still be
selected and copied; a failed `xdg-open` reports once in the status line. None
of these is a reason to lose a message.

## Testing

The probe is a pure function of an environment map — tested by table, one row
per terminal, with the resolved capability asserted.

Encoders are tested on their bytes: a decoded fixture in, an escape sequence or
a block string out, with the downscale cap asserted at the boundary. Remote
fetching is tested against the allowlist with no network — an allowed host and a
refused one, asserting the refused one is named and not fetched.

Link detection is a table of transcript lines to expected spans, including the
markdown-label-lies case, which asserts the status line shows the resolved
target and not the label. Opening is tested with an injected launcher: the
gesture calls it, an arriving message never does.

The screens and the search are tested like the rest of `tui/` — driving the
model with key messages and asserting on the rendered frame.
