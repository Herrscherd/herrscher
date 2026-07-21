package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestBlockWrapsToWidth checks a long body is folded so no rendered line exceeds
// the content width, and every wrapped line carries the gutter spine.
func TestBlockWrapsToWidth(t *testing.T) {
	body := strings.Repeat("lorem ipsum dolor ", 20)
	out := renderEntry(entry{role: roleAgent, text: body}, 40)
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("long body must wrap to several lines, got %d", len(lines))
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > 40 {
			t.Fatalf("line %d width %d exceeds 40: %q", i, w, ln)
		}
	}
	// Every body line (after the header) begins with the gutter glyph.
	for _, ln := range lines[1:] {
		if !strings.Contains(ln, glyphGutter) {
			t.Fatalf("wrapped body line missing gutter: %q", ln)
		}
	}
}

// TestWideGlyphWrapDoesNotOverflow guards the glyph-width awareness: a run of
// double-width runes must still fold within the content width.
func TestWideGlyphWrapDoesNotOverflow(t *testing.T) {
	out := renderEntry(entry{role: roleYou, text: strings.Repeat("世界", 30)}, 24)
	for _, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w > 24 {
			t.Fatalf("wide-glyph line width %d exceeds 24: %q", w, ln)
		}
	}
}

// TestChunkCoalescingRendersOneBlock verifies streamed chunks accumulate into a
// single agent entry so the final wrap spans the whole message.
func TestChunkCoalescingRendersOneBlock(t *testing.T) {
	tb := &tab{}
	tb.appendChunk("hello ")
	tb.appendChunk("world")
	if len(tb.entries) != 1 {
		t.Fatalf("chunks must coalesce into one entry, got %d", len(tb.entries))
	}
	if tb.entries[0].text != "hello world" {
		t.Fatalf("coalesced text = %q", tb.entries[0].text)
	}
	// A status line between streams breaks the block; the next chunk opens a new one.
	tb.endStream()
	tb.appendEntry(entry{role: roleStatus, text: "tool"})
	tb.appendChunk("again")
	if len(tb.entries) != 3 {
		t.Fatalf("interrupted stream must open a fresh block: %d entries", len(tb.entries))
	}
}

// TestRenderTranscriptResizeReflows confirms the same entries wrap differently at
// two widths — i.e. wrapping is recomputed, not frozen.
func TestRenderTranscriptResizeReflows(t *testing.T) {
	m := newTestModel()
	tb := m.ensureTab("a")
	tb.appendEntry(entry{role: roleAgent, text: strings.Repeat("word ", 40)})
	narrow := strings.Count(m.renderTranscript(tb, 20), "\n")
	wide := strings.Count(m.renderTranscript(tb, 100), "\n")
	if narrow <= wide {
		t.Fatalf("narrow width must wrap to more lines: narrow=%d wide=%d", narrow, wide)
	}
}
