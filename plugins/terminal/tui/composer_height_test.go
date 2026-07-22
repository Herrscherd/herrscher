package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
)

// newComposerLikeArea builds a textarea configured exactly as the live composer
// (no prompt gutter, no line numbers) so its content width and wrap match.
func newComposerLikeArea(width int) textarea.Model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.SetWidth(width)
	return ta
}

// TestComposerWrapMatchesWidget pins composerWrap to the real widget's soft-wrap:
// LineInfo().Height is the widget's own wrapped-row count for the cursor's logical
// line, so for a single line it must equal len(composerWrap(...)).
func TestComposerWrapMatchesWidget(t *testing.T) {
	cases := []struct {
		name  string
		width int
		value string
	}{
		{"empty", 20, ""},
		{"short", 20, "hello"},
		{"exact fill", 10, "0123456789"},
		{"one over", 10, "0123456789a"},
		{"two words wrap", 10, "hello world again"},
		{"long unbroken", 8, "abcdefghijklmnopqrstuvwxyz"},
		{"trailing spaces", 10, "hi        "},
		{"wide runes", 10, "日本語テストです"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ta := newComposerLikeArea(c.width)
			ta.SetValue(c.value)
			want := ta.LineInfo().Height
			got := len(composerWrap([]rune(c.value), ta.Width()))
			if got != want {
				t.Fatalf("composerWrap rows = %d, widget LineInfo().Height = %d (width=%d value=%q)",
					got, want, ta.Width(), c.value)
			}
		})
	}
}

// TestDisplayRowsGrowsWithWrap is the behavioural guard: a line longer than the
// width must report more than one row, and explicit newlines add rows.
func TestDisplayRowsGrowsWithWrap(t *testing.T) {
	if n := displayRows("", 20); n != 1 {
		t.Fatalf("empty displayRows = %d, want 1", n)
	}
	if n := displayRows("short", 20); n != 1 {
		t.Fatalf("short displayRows = %d, want 1", n)
	}
	long := strings.Repeat("word ", 20)
	if n := displayRows(long, 20); n < 2 {
		t.Fatalf("long line displayRows = %d, want >= 2", n)
	}
	if n := displayRows("a\nb\nc", 20); n != 3 {
		t.Fatalf("three logical lines displayRows = %d, want 3", n)
	}
	if n := displayRows("anything", 0); n != 1 {
		t.Fatalf("zero width displayRows = %d, want 1 (logical fallback)", n)
	}
}
