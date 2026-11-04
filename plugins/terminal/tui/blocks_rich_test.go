package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/charmbracelet/x/ansi"
)

func TestDiffUsesBasicColourOnA16ColourTerminal(t *testing.T) {
	out := renderDiff("@@ -1 +1 @@\n+added\n-gone", 40, view{caps: Capabilities{Colour: Colour16}, st: testStyles()})
	if !strings.Contains(out, "\x1b[32m") || !strings.Contains(out, "\x1b[31m") {
		t.Fatalf("a 16-colour terminal must get the basic green/red pair: %q", out)
	}
}

func TestDiffWithoutAPreambleIsAllContent(t *testing.T) {
	out := renderDiff("+added\n-gone", 40, view{caps: Capabilities{Colour: Colour16}, st: testStyles()})
	if !strings.Contains(out, ansiGreen) || !strings.Contains(out, ansiRed) {
		t.Fatalf("a bare hunk must still be coloured: %q", out)
	}
}

func TestDiffLinesAreClippedToWidth(t *testing.T) {
	out := renderDiff("@@ -1 +1 @@\n+"+strings.Repeat("x", 80), 20, view{caps: Capabilities{Colour: ColourTrue}, st: testStyles()})
	for _, ln := range strings.Split(out, "\n") {
		if lipgloss.Width(ln) > 20 {
			t.Fatalf("a diff line must be clipped to the width: %q", ln)
		}
	}
}

func TestTableFitsItsWidthByTruncatingTheWidestCell(t *testing.T) {
	rows := [][]string{
		{"name", "description"},
		{"ok", strings.Repeat("very long ", 20)},
	}
	out := renderTable(rows, 40, testStyles())
	for _, ln := range strings.Split(out, "\n") {
		if lipgloss.Width(ln) > 40 {
			t.Fatalf("row wider than the table: %d cols in %q", lipgloss.Width(ln), ln)
		}
	}
	if n := len(strings.Split(out, "\n")); n != len(rows)+1 {
		t.Fatalf("a truncated table must keep one row per row (plus the rule), got %d lines", n)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("a narrow column must survive its wide neighbour: %q", out)
	}
}

func TestTableShrinksTheWidestColumnFirst(t *testing.T) {
	rows := [][]string{{"a", strings.Repeat("w", 60)}, {"b", "short"}}
	out := renderTable(rows, 30, testStyles())
	if !strings.Contains(out, "a") || !strings.Contains(out, "b") {
		t.Fatalf("the narrow column must be left intact: %q", out)
	}
}

func TestFoldedCodeBlockCollapsesAndRestoresExactly(t *testing.T) {
	code := strings.Repeat("line\n", foldThreshold+5)
	text := "before\n```go\n" + code + "```\nafter"

	folded := foldCodeBlocks(text, unicodeGlyphs())
	if strings.Contains(folded, strings.Repeat("line\n", 2)) {
		t.Fatalf("a folded block must not still contain its body: %q", folded)
	}
	if !strings.Contains(folded, "go") || !strings.Contains(folded, "lines") {
		t.Fatalf("the summary must name the language and the size: %q", folded)
	}
	if !strings.Contains(folded, "before") || !strings.Contains(folded, "after") {
		t.Fatalf("folding must leave the prose around the block alone: %q", folded)
	}
	if got := len(strings.Split(folded, "\n")); got != 3 {
		t.Fatalf("a folded block is one line: got %d lines in %q", got, folded)
	}
	if foldCodeBlocks(text, unicodeGlyphs()) == text {
		t.Fatalf("the fixture must actually fold")
	}
}

func TestShortCodeBlocksAreNotFolded(t *testing.T) {
	text := "```go\nfmt.Println(1)\n```"
	if foldCodeBlocks(text, unicodeGlyphs()) != text {
		t.Fatalf("a short block must be left alone: %q", foldCodeBlocks(text, unicodeGlyphs()))
	}
}

func TestCopyPutsTheRawBlockOnTheClipboard(t *testing.T) {
	m := newTestModel()
	clip := &fakeWriteClipboard{}
	m.clip = clip
	tb := m.ensureTab("a")
	m.active = "a"
	tb.appendEntry(entry{role: roleAgent, text: "here:\n```go\nfmt.Println(1)\n```\ndone"})

	m.copyLastCode()
	if clip.wrote != "fmt.Println(1)" {
		t.Fatalf("the clipboard must get the raw block, got %q", clip.wrote)
	}
	if !strings.Contains(m.flash, "copiée") {
		t.Fatalf("the copy must be acknowledged: %q", m.flash)
	}
}

func TestCopyWithNoCodeBlockSaysSo(t *testing.T) {
	m := newTestModel()
	m.clip = &fakeWriteClipboard{}
	tb := m.ensureTab("a")
	m.active = "a"
	tb.appendEntry(entry{role: roleAgent, text: "no code here"})

	m.copyLastCode()
	if m.flash == "" || strings.Contains(m.flash, "copiée") {
		t.Fatalf("an empty copy must say why: %q", m.flash)
	}
}

func TestCodeBlocksAreFoundWithTheirLanguage(t *testing.T) {
	got := codeBlocks("a\n```go\nx := 1\n```\nb\n```\nplain\n```")
	if len(got) != 2 {
		t.Fatalf("codeBlocks = %+v", got)
	}
	if got[0].lang != "go" || got[0].body != "x := 1" {
		t.Errorf("first block = %+v", got[0])
	}
	if got[1].lang != "" || got[1].body != "plain" {
		t.Errorf("second block = %+v", got[1])
	}
}

type fakeWriteClipboard struct{ wrote string }

func (f *fakeWriteClipboard) ImageType(context.Context) (string, bool)          { return "", false }
func (f *fakeWriteClipboard) ReadImage(context.Context, string) ([]byte, error) { return nil, nil }
func (f *fakeWriteClipboard) WriteText(_ context.Context, s string) error       { f.wrote = s; return nil }

func TestDiffKeepsFourHunkLinesAndFolds(t *testing.T) {
	src := strings.Join([]string{
		"@@ -12,7 +12,9 @@ func Register(",
		"+  a", "-  b", "+  c", "+  d", "+  e", "+  f",
	}, "\n")
	out := renderDiff(src, 74, view{caps: Capabilities{}, st: testStyles()})
	lines := strings.Split(out, "\n")
	if len(lines) != diffHunkBudget+2 {
		t.Fatalf("got %d lines, want %d: %q", len(lines), diffHunkBudget+2, lines)
	}
	last := ansi.Strip(lines[len(lines)-1])
	if !strings.Contains(last, unicodeGlyphs().fold) || !strings.Contains(last, "2 lignes de plus") {
		t.Fatalf("fold line = %q", last)
	}
}

func TestDiffUnderBudgetHasNoFold(t *testing.T) {
	out := renderDiff("@@ -1,2 +1,2 @@\n+  a\n-  b", 74, view{caps: Capabilities{}, st: testStyles()})
	if strings.Contains(ansi.Strip(out), unicodeGlyphs().fold) {
		t.Fatalf("short diff folded: %q", out)
	}
}

func TestDiffExpandsWithAltE(t *testing.T) {
	src := strings.Join([]string{
		"@@ -12,7 +12,9 @@ func Register(",
		"+  a", "-  b", "+  c", "+  d", "+  e", "+  f",
	}, "\n")
	out := renderDiff(src, 74, view{st: testStyles(), expand: true})
	if strings.Contains(ansi.Strip(out), unicodeGlyphs().fold) {
		t.Fatalf("alt+e must lift the diff budget: %q", out)
	}
	if got := len(strings.Split(out, "\n")); got != 7 {
		t.Fatalf("expanded diff drew %d lines, want 7", got)
	}
}
