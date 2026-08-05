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
		out := renderEntry(entry{role: roleAgent, text: c.in}, 60)
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
	out := renderEntry(entry{role: roleAgent, text: in, streaming: true}, 60)
	if !strings.Contains(out, "```go") {
		t.Fatalf("a streaming block must render raw: %q", out)
	}
}

// TestMarkdownRewrapsOnWidth guards the per-width renderer cache: the same text
// at two widths must fold differently, not reuse the first width's wrap.
func TestMarkdownRewrapsOnWidth(t *testing.T) {
	body := strings.Repeat("lorem ipsum dolor sit amet ", 10)
	narrow := renderMarkdown(body, 30)
	wide := renderMarkdown(body, 90)
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
