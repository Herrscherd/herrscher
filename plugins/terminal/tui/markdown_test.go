package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestMarkdownRendersStructure checks the engine is actually wired: the source
// markers must not survive into the output, which is what used to happen.
func TestMarkdownRendersStructure(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"heading", "# Title", "Title"},
		{"list", "- one\n- two", "one"},
		{"inline span", "use `errPrefix` here", "errPrefix"},
		// Chroma tokenises the block, so only a single token survives contiguously.
		{"code block", "```go\nfmt.Println(1)\n```", "Println"},
	}
	for _, c := range cases {
		out := renderEntry(entry{role: roleAgent, text: c.in}, 60, view{})
		if !strings.Contains(out, c.want) {
			t.Errorf("%s: rendered output must keep its content, got %q", c.name, out)
		}
		if strings.Contains(out, "```") || strings.Contains(out, "# ") {
			t.Errorf("%s: markdown source markers must not survive: %q", c.name, out)
		}
	}
}

// TestStreamingMarkdownStaysRaw locks the streaming rule: a fence that has not
// closed yet must not be handed to the engine, which would eat the rest.
func TestStreamingMarkdownStaysRaw(t *testing.T) {
	in := "here it is:\n```go\nfmt.Println("
	out := renderEntry(entry{role: roleAgent, text: in, streaming: true}, 60, view{})
	if !strings.Contains(out, "```go") {
		t.Fatalf("a streaming block must render raw: %q", out)
	}
}

// TestMarkdownRewrapsOnWidth guards the per-width renderer cache: the same text
// at two widths must fold differently, not reuse the first width's wrap.
func TestMarkdownRewrapsOnWidth(t *testing.T) {
	body := strings.Repeat("lorem ipsum dolor sit amet ", 10)
	narrow := renderMarkdown(body, 30, Capabilities{})
	wide := renderMarkdown(body, 90, Capabilities{})
	if strings.Count(narrow, "\n") <= strings.Count(wide, "\n") {
		t.Fatalf("narrow must wrap to more lines: narrow=%d wide=%d",
			strings.Count(narrow, "\n"), strings.Count(wide, "\n"))
	}
	for _, ln := range strings.Split(narrow, "\n") {
		if w := lipgloss.Width(ln); w > 30 {
			t.Fatalf("markdown line width %d exceeds 30: %q", w, ln)
		}
	}
}

// TestDiffBlockColoursItsLineClasses locks the one place colour carries meaning
// rather than syntax: an addition, a removal and a hunk header must not render
// alike, and chroma must not get its hands on them.
func TestDiffBlockColoursItsLineClasses(t *testing.T) {
	in := "before:\n```diff\n@@ -1,2 +1,2 @@\n-old line\n+new line\n context\n```\nafter."
	out := renderEntry(entry{role: roleAgent, text: in}, 60, view{})
	add := greenStyle.Render("+new line")
	del := redStyle.Render("-old line")
	if !strings.Contains(out, add) {
		t.Errorf("an added line must render green: %q", out)
	}
	if !strings.Contains(out, del) {
		t.Errorf("a removed line must render red: %q", out)
	}
	if !strings.Contains(out, dimStyle.Render("@@ -1,2 +1,2 @@")) {
		t.Errorf("a hunk header must render dim: %q", out)
	}
	for _, want := range []string{"before", "after"} {
		if !strings.Contains(out, want) {
			t.Errorf("prose around the diff must survive (%q): %q", want, out)
		}
	}
}

// TestNonDiffBlockKeepsTheEngine checks the interception is narrow: a go block
// still goes through glamour, so only diffs take the hand-rendered path.
func TestNonDiffBlockKeepsTheEngine(t *testing.T) {
	segs := splitDiffs("```go\n-not a removal\n```")
	if len(segs) != 1 || segs[0].diff {
		t.Fatalf("a go block must stay one prose segment, got %+v", segs)
	}
}

// TestUnclosedDiffFenceTakesTheRest guards the streaming case: a fence that has
// not closed yet still renders as a diff rather than leaking its marker.
func TestUnclosedDiffFenceTakesTheRest(t *testing.T) {
	segs := splitDiffs("look:\n```diff\n+half a line")
	if len(segs) != 2 || !segs[1].diff || segs[1].text != "+half a line" {
		t.Fatalf("an unclosed diff fence must take the rest: %+v", segs)
	}
}

// TestDiffLineIsClippedNotWrapped keeps a folded diff line from losing the +/-
// that says what it is.
func TestDiffLineIsClippedNotWrapped(t *testing.T) {
	out := renderDiff("+"+strings.Repeat("x", 80), 20, Capabilities{})
	if strings.Contains(out, "\n") {
		t.Fatalf("a diff line must be clipped, not folded: %q", out)
	}
	if w := lipgloss.Width(out); w > 20 {
		t.Fatalf("clipped diff line width %d exceeds 20: %q", w, out)
	}
}
