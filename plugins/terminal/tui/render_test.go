package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestUserEntryIsOneUnruledRow checks a user turn costs exactly the rows its text
// needs. It used to be framed between two rules, which meant a one-word message
// took four rows for one character — and a frame drawn around every turn marks
// nothing. The glyph and the colour are what tell it apart.
func TestUserEntryIsOneUnruledRow(t *testing.T) {
	out := renderEntry(entry{role: roleYou, text: "hello there"}, 40, view{})
	if !strings.Contains(out, glyphCursor+" hello there") {
		t.Fatalf("user entry must render its request: %q", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 1 {
		t.Fatalf("a one-line request must take one row, got %d: %q", len(lines), out)
	}
	if strings.Contains(out, strings.Repeat(ruleChar, 4)) {
		t.Fatalf("user entry must carry no rule: %q", out)
	}
	if strings.Contains(out, glyphTool) || strings.Contains(out, "▎") {
		t.Fatalf("user entry must carry no tool/gutter glyph: %q", out)
	}
}

// TestAgentEntryRendersBare checks agent prose renders full-width with no prompt
// or tool prefix.
func TestAgentEntryRendersBare(t *testing.T) {
	out := renderEntry(entry{role: roleAgent, text: "the answer"}, 40, view{})
	if !strings.Contains(out, "the answer") {
		t.Fatalf("agent prose must render its text: %q", out)
	}
	if strings.Contains(out, glyphPrompt) || strings.Contains(out, glyphTool) {
		t.Fatalf("agent prose must be bare, no prompt/tool glyph: %q", out)
	}
}

// TestRoleGlyphsAreDistinct is the point of having roles at all: reasoning, a
// notice and an error must not render as the same line the way they used to.
func TestRoleGlyphsAreDistinct(t *testing.T) {
	think := renderEntry(entry{role: roleThinking, text: "weighing two options"}, 60, view{})
	notice := renderEntry(entry{role: roleNotice, text: "turn reset"}, 60, view{})
	fail := renderEntry(entry{role: roleError, text: "exit status 1"}, 60, view{})
	if !strings.Contains(think, glyphThinking) {
		t.Fatalf("reasoning must carry its glyph: %q", think)
	}
	if !strings.Contains(notice, glyphNotice+" turn reset") {
		t.Fatalf("a notice must carry its glyph: %q", notice)
	}
	// The error's glyph titles its rule rather than sitting beside the text: the
	// whole block is the error, not just its first line.
	if !strings.Contains(fail, glyphError) || !strings.Contains(fail, "exit status 1") {
		t.Fatalf("an error must carry its glyph and its text: %q", fail)
	}
}

// TestStatusRoleSplitsErrorsFromTools locks the one distinction the bus does not
// make for us: tool calls and host errors arrive on the same event.
func TestStatusRoleSplitsErrorsFromTools(t *testing.T) {
	if got := statusRole("Read core/parse.go"); got != roleTool {
		t.Fatalf("a tool line is a tool: %q", got)
	}
	if got := statusRole(hostErrPrefix + "the worktree is dirty"); got != roleError {
		t.Fatalf("a host-flagged line is an error: %q", got)
	}
}

// TestAgentBodyWrapsToWidth checks a long agent body folds so no rendered line
// exceeds the content width.
func TestAgentBodyWrapsToWidth(t *testing.T) {
	body := strings.Repeat("lorem ipsum dolor ", 20)
	out := renderEntry(entry{role: roleAgent, text: body}, 40, view{})
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("long body must wrap to several lines, got %d", len(lines))
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > 40 {
			t.Fatalf("line %d width %d exceeds 40: %q", i, w, ln)
		}
	}
}

// TestWideGlyphWrapDoesNotOverflow guards the glyph-width awareness: a run of
// double-width runes must still fold within the content width.
func TestWideGlyphWrapDoesNotOverflow(t *testing.T) {
	out := renderEntry(entry{role: roleYou, text: strings.Repeat("世界", 30)}, 24, view{})
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
	tb.appendEntry(entry{role: roleTool, text: "tool"})
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
