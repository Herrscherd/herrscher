package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// A diff's colour is its meaning: which line was added and which was removed.
// The file headers of a unified diff start with the same characters and mean
// something else entirely, so classification is scoped to the hunk.
func TestDiffClassifiesPerHunk(t *testing.T) {
	body := strings.Join([]string{
		"--- a/main.go",
		"+++ b/main.go",
		"@@ -1,4 +1,4 @@",
		" context stays plain",
		"-gone",
		"+arrived",
	}, "\n")

	want := []string{diffMeta, diffMeta, diffMeta, diffContext, diffDel, diffAdd}
	for i, ln := range strings.Split(body, "\n") {
		inHunk := i >= 3
		if got := diffClass(ln, inHunk); got != want[i] {
			t.Errorf("diffClass(%q, %v) = %q, want %q", ln, inHunk, got, want[i])
		}
	}
}

// Outside a hunk a "+" is the header of the file it names, not an addition, and
// painting it green says the whole file was added.
func TestDiffHeadersAreNotAdditions(t *testing.T) {
	if got := diffClass("+++ b/main.go", false); got == diffAdd {
		t.Errorf("a file header must not classify as an addition: %q", got)
	}
}

// On a 16-colour terminal the palette's hex green is approximated to whatever is
// nearest, which for a diff can be nothing at all. The basic ANSI pair is the one
// every terminal has agreed on since the seventies.
func TestDiffUsesBasicColourOnA16ColourTerminal(t *testing.T) {
	out := renderDiff("@@ -1 +1 @@\n+added\n-gone", 40, Capabilities{Colour: Colour16})
	if !strings.Contains(out, "\x1b[32m") || !strings.Contains(out, "\x1b[31m") {
		t.Fatalf("a 16-colour terminal must get the basic green/red pair: %q", out)
	}
}

// An agent quoting a change writes the hunk and not the preamble, and that is
// the common case: those lines are content, not headers.
func TestDiffWithoutAPreambleIsAllContent(t *testing.T) {
	out := renderDiff("+added\n-gone", 40, Capabilities{Colour: Colour16})
	if !strings.Contains(out, ansiGreen) || !strings.Contains(out, ansiRed) {
		t.Fatalf("a bare hunk must still be coloured: %q", out)
	}
}

// A diff line is clipped, never folded: a wrapped continuation has no leading
// +/- and reads as context.
func TestDiffLinesAreClippedToWidth(t *testing.T) {
	out := renderDiff("@@ -1 +1 @@\n+"+strings.Repeat("x", 80), 20, Capabilities{Colour: ColourTrue})
	for _, ln := range strings.Split(out, "\n") {
		if lipgloss.Width(ln) > 20 {
			t.Fatalf("a diff line must be clipped to the width: %q", ln)
		}
	}
}

// A table that wraps stops being a table: the columns no longer line up and the
// reader has to reassemble the rows by eye. Truncating the widest cell keeps the
// shape, which is the only reason to draw a table at all.
func TestTableFitsItsWidthByTruncatingTheWidestCell(t *testing.T) {
	rows := [][]string{
		{"name", "description"},
		{"ok", strings.Repeat("very long ", 20)},
	}
	out := renderTable(rows, 40)
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

// The narrow columns are not the problem, so they are not what shrinks.
func TestTableShrinksTheWidestColumnFirst(t *testing.T) {
	rows := [][]string{{"a", strings.Repeat("w", 60)}, {"b", "short"}}
	out := renderTable(rows, 30)
	if !strings.Contains(out, "a") || !strings.Contains(out, "b") {
		t.Fatalf("the narrow column must be left intact: %q", out)
	}
}

// A long code block buries the answer that follows it. Folded, it says what it is
// and how much of it there is; unfolded, it is byte for byte what it was.
func TestFoldedCodeBlockCollapsesAndRestoresExactly(t *testing.T) {
	code := strings.Repeat("line\n", foldThreshold+5)
	text := "before\n```go\n" + code + "```\nafter"

	folded := foldCodeBlocks(text)
	if strings.Contains(folded, "line\nline") {
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
	// Unfolding is not a re-render: the original text is what is kept.
	if foldCodeBlocks(text) == text {
		t.Fatalf("the fixture must actually fold")
	}
}

// A block short enough to read is not worth hiding.
func TestShortCodeBlocksAreNotFolded(t *testing.T) {
	text := "```go\nfmt.Println(1)\n```"
	if foldCodeBlocks(text) != text {
		t.Fatalf("a short block must be left alone: %q", foldCodeBlocks(text))
	}
}

// Copy hands over the block's raw text — not the rendered one. What lands on the
// clipboard has to compile.
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
	if !strings.Contains(m.flash, "copied") {
		t.Fatalf("the copy must be acknowledged: %q", m.flash)
	}
}

// Nothing to copy is a message, not a silence and not an error.
func TestCopyWithNoCodeBlockSaysSo(t *testing.T) {
	m := newTestModel()
	m.clip = &fakeWriteClipboard{}
	tb := m.ensureTab("a")
	m.active = "a"
	tb.appendEntry(entry{role: roleAgent, text: "no code here"})

	m.copyLastCode()
	if m.flash == "" || strings.Contains(m.flash, "copied") {
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

// fakeWriteClipboard records what was copied and holds no image.
type fakeWriteClipboard struct{ wrote string }

func (f *fakeWriteClipboard) ImageType() (string, bool)        { return "", false }
func (f *fakeWriteClipboard) ReadImage(string) ([]byte, error) { return nil, nil }
func (f *fakeWriteClipboard) WriteText(s string) error         { f.wrote = s; return nil }
