package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

// The agent writes markdown — headings, lists, inline spans, fenced code. Until
// now the TUI printed the source characters, so a code block read as a wall of
// backticks. glamour turns it into structure; this file is the whole of that
// coupling, so swapping the engine later touches one file.

// mdRenderer caches the renderer for the width it was built at. glamour fixes
// its word-wrap at construction and recompiles its style each time, so building
// one per frame would be visible; building one per *width* costs a resize.
var (
	mdRenderer *glamour.TermRenderer
	mdWidth    int
)

// mdStyle is the dark style with glamour's own framing removed — no page margin,
// no leading and trailing blank line — and its headings pulled onto the palette.
// Every transcript entry is already positioned by the transcript renderer, so the
// engine must contribute structure and nothing else.
func mdStyle() ansi.StyleConfig {
	s := styles.DarkStyleConfig
	zero := uint(0)
	s.Document.Margin = &zero
	s.Document.BlockPrefix = ""
	s.Document.BlockSuffix = ""
	// Body prose keeps the terminal's own foreground. Colouring the document
	// makes glamour wrap every word in its own escape pair, which triples the
	// size of a transcript and defeats any substring search over it; the
	// difference on screen is a shade of grey.
	s.Document.Color = nil
	s.Heading.Color = strPtr(colViolet)
	s.H1.BackgroundColor = nil
	s.H1.Color = strPtr(colViolet)
	s.CodeBlock.Margin = &zero
	return s
}

func strPtr(s string) *string { return &s }

// renderMarkdown renders agent prose as markdown wrapped to width, rendering any
// fenced diff by hand on the way. A diff is the one block where colour carries
// meaning rather than syntax: chroma would paint it as source code and lose the
// only distinction that matters, which line was added and which was removed.
func renderMarkdown(text string, width int, caps Capabilities) string {
	var b strings.Builder
	for i, seg := range splitDiffs(text) {
		if i > 0 {
			b.WriteByte('\n')
		}
		if seg.diff {
			b.WriteString(renderDiff(seg.text, width, caps))
			continue
		}
		b.WriteString(renderProse(seg.text, width))
	}
	return b.String()
}

// renderProse renders one non-diff segment, pulling pipe tables out of it. The
// markdown engine draws a table by wrapping its cells, which at terminal widths
// turns a comparison into confetti; renderTable keeps the columns and gives up
// the text that did not fit instead.
func renderProse(text string, width int) string {
	var b strings.Builder
	var prose, table []string
	flushProse := func() {
		if len(prose) > 0 {
			b.WriteString(renderGlamour(strings.Join(prose, "\n"), width))
			prose = nil
		}
	}
	flushTable := func() {
		if len(table) > 0 {
			b.WriteString(renderTable(parseTableRows(table), width) + "\n")
			table = nil
		}
	}
	for _, ln := range strings.Split(text, "\n") {
		if isTableRow(ln) {
			flushProse()
			table = append(table, ln)
			continue
		}
		flushTable()
		prose = append(prose, ln)
	}
	flushTable()
	flushProse()
	return strings.Trim(b.String(), "\n")
}

// segment is a run of prose or the body of one fenced diff block.
type segment struct {
	text string
	diff bool
}

// splitDiffs cuts text into alternating prose and diff-body segments. An unclosed
// diff fence takes the rest of the message: the agent is mid-stream or wrote a
// broken fence, and either way those lines are a diff.
func splitDiffs(text string) []segment {
	lines := strings.Split(text, "\n")
	var segs []segment
	var buf []string
	inDiff := false
	flush := func(diff bool) {
		if len(buf) > 0 {
			segs = append(segs, segment{text: strings.Join(buf, "\n"), diff: diff})
			buf = nil
		}
	}
	for _, ln := range lines {
		fence := strings.TrimSpace(ln)
		switch {
		case !inDiff && (fence == "```diff" || fence == "~~~diff"):
			flush(false)
			inDiff = true
		case inDiff && (fence == "```" || fence == "~~~"):
			flush(true)
			inDiff = false
		default:
			buf = append(buf, ln)
		}
	}
	flush(inDiff)
	if len(segs) == 0 {
		return []segment{{text: text}}
	}
	return segs
}

// renderGlamour runs prose through the markdown engine. Anything the engine
// refuses — and it refuses nothing in practice, but a nil renderer would panic —
// falls back to the plain wrap, so prose always reaches the screen.
func renderGlamour(text string, width int) string {
	if width < 1 {
		width = 1
	}
	if mdRenderer == nil || mdWidth != width {
		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(mdStyle()),
			glamour.WithWordWrap(width),
			glamour.WithEmoji(),
		)
		if err != nil {
			return wrapWith(textStyle, text, width)
		}
		mdRenderer, mdWidth = r, width
	}
	out, err := mdRenderer.Render(text)
	if err != nil {
		return wrapWith(textStyle, text, width)
	}
	// glamour pads every line out to the wrap width and frames the document with
	// blank lines. The transcript owns its own spacing, and trailing padding
	// breaks the terminal's native text selection: strip both.
	lines := strings.Split(out, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " ")
	}
	// Trim after de-padding: a frame line of pure spaces only becomes an empty
	// line once its padding is gone, and it is the empty lines we want dropped.
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}
