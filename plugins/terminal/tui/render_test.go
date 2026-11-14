package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestUserEntryIsOneUnruledRow(t *testing.T) {
	out := renderEntry(entry{role: roleYou, text: "hello there"}, 40, view{tr: newTranslator(), st: testStyles()})
	if !strings.Contains(out, unicodeGlyphs().userBar+" hello there") {
		t.Fatalf("user entry must render its request: %q", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 1 {
		t.Fatalf("a one-line request must take one row, got %d: %q", len(lines), out)
	}
	if strings.Contains(out, strings.Repeat(unicodeGlyphs().ruleChar, 4)) {
		t.Fatalf("user entry must carry no rule: %q", out)
	}
	if strings.Contains(out, unicodeGlyphs().tool) || strings.Contains(out, "▎") {
		t.Fatalf("user entry must carry no tool/gutter glyph: %q", out)
	}
}

func TestAgentEntryRendersBare(t *testing.T) {
	out := renderEntry(entry{role: roleAgent, text: "the answer"}, 40, view{tr: newTranslator(), st: testStyles()})
	if !strings.Contains(out, "the answer") {
		t.Fatalf("agent prose must render its text: %q", out)
	}
	if !strings.Contains(out, unicodeGlyphs().tool+" the answer") {
		t.Fatalf("agent prose must open on the prose gutter: %q", out)
	}
}

func TestRoleGlyphsAreDistinct(t *testing.T) {
	think := renderEntry(entry{role: roleThinking, text: "weighing two options"}, 60, view{tr: newTranslator(), st: testStyles()})
	notice := renderEntry(entry{role: roleNotice, text: "tour reinitialise"}, 60, view{tr: newTranslator(), st: testStyles()})
	fail := renderEntry(entry{role: roleError, text: "exit status 1"}, 60, view{tr: newTranslator(), st: testStyles()})
	if !strings.Contains(think, unicodeGlyphs().fold) {
		t.Fatalf("reasoning must carry its glyph: %q", think)
	}
	if !strings.Contains(notice, unicodeGlyphs().notice+" tour reinitialise") {
		t.Fatalf("a notice must carry its glyph: %q", notice)
	}
	if !strings.Contains(fail, unicodeGlyphs().failure) || !strings.Contains(fail, "exit status 1") {
		t.Fatalf("an error must carry its glyph and its text: %q", fail)
	}
}

func TestStatusRoleSplitsErrorsFromTools(t *testing.T) {
	if got := statusRole("Read core/parse.go"); got != roleTool {
		t.Fatalf("a tool line is a tool: %q", got)
	}
	if got := statusRole(hostErrPrefix + "the worktree is dirty"); got != roleError {
		t.Fatalf("a host-flagged line is an error: %q", got)
	}
}

func TestAgentBodyWrapsToWidth(t *testing.T) {
	body := strings.Repeat("lorem ipsum dolor ", 20)
	out := renderEntry(entry{role: roleAgent, text: body}, 40, view{tr: newTranslator(), st: testStyles()})
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

func TestWideGlyphWrapDoesNotOverflow(t *testing.T) {
	out := renderEntry(entry{role: roleYou, text: strings.Repeat("世界", 30)}, 24, view{tr: newTranslator(), st: testStyles()})
	for _, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w > 24 {
			t.Fatalf("wide-glyph line width %d exceeds 24: %q", w, ln)
		}
	}
}

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
	tb.endStream()
	tb.appendEntry(entry{role: roleTool, text: "tool"})
	tb.appendChunk("again")
	if len(tb.entries) != 3 {
		t.Fatalf("interrupted stream must open a fresh block: %d entries", len(tb.entries))
	}
}

func TestNewTextBlockOpensWithoutItsSeparator(t *testing.T) {
	tb := &tab{}
	tb.appendChunk("Je regarde.")
	tb.endStream()
	tb.appendEntry(entry{role: roleTool, text: "read"})
	tb.appendChunk("\n\nVoici")
	if len(tb.entries) != 3 {
		t.Fatalf("a text block after a tool block must open its own entry: %d entries", len(tb.entries))
	}
	if got := tb.entries[2].text; got != "Voici" {
		t.Fatalf("a new text entry must not open on blank lines: %q", got)
	}
}

func TestContinuedTextKeepsItsSeparator(t *testing.T) {
	tb := &tab{}
	tb.appendChunk("Je regarde.")
	tb.appendChunk("\n\nVoici")
	if got := tb.entries[0].text; got != "Je regarde.\n\nVoici" {
		t.Fatalf("a chunk that extends a streaming entry must keep its separator: %q", got)
	}
}

func TestBlankOpeningChunkOpensNoEntry(t *testing.T) {
	for _, blank := range []string{"\n", "  \n", " \t\n\n"} {
		tb := &tab{}
		tb.appendChunk(blank)
		if len(tb.entries) != 0 {
			t.Fatalf("a chunk that is only whitespace must open no entry, got %d entries for %q", len(tb.entries), blank)
		}
	}
}

func openedFrom(chunks []string) *tab {
	tb := &tab{}
	for _, chunk := range chunks {
		tb.appendChunk(chunk)
	}
	return tb
}

func TestFragmentsOpenTheEntryWithTheirIndent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []string
		want   string
	}{
		{"blank lines", []string{"\n\nVoici"}, "Voici"},
		{"blank lines and spaces", []string{"  \n\nVoici"}, "Voici"},
		{"tabs and newlines", []string{" \t\n  \nVoici"}, "Voici"},
		{"indent after a newline", []string{"\n    code"}, "    code"},
		{"indent without a newline", []string{"   code"}, "   code"},
		{"newline then spaces fragments", []string{"\n", "    ", "code"}, "    code"},
		{"spaces fragment then word", []string{"   ", "code"}, "   code"},
		{"newline and tab fragments", []string{"\n\t", "code"}, "\tcode"},
	} {
		for _, chunks := range [][]string{tc.chunks, {strings.Join(tc.chunks, "")}} {
			tb := openedFrom(chunks)
			if len(tb.entries) != 1 {
				t.Fatalf("%s: the next non-empty chunk must open one entry from %q: %d entries", tc.name, chunks, len(tb.entries))
			}
			if got := tb.entries[0].text; got != tc.want || !tb.entries[0].streaming {
				t.Fatalf("%s: the entry from %q must open on %q: %+v", tc.name, chunks, tc.want, tb.entries[0])
			}
		}
	}
}

func TestPendingBlankOpensNoEntryAtTurnEnd(t *testing.T) {
	tb := &tab{}
	tb.appendChunk("\n")
	tb.appendChunk("   ")
	tb.endStream()
	if len(tb.entries) != 0 {
		t.Fatalf("blank fragments closed by the end of the turn must open no entry: %d entries", len(tb.entries))
	}
}

func TestPendingBlankDoesNotCarryPastAClosingEvent(t *testing.T) {
	for name, closing := range map[string]func(*tab){
		"end of turn": (*tab).endStream,
		"tool entry":  func(tb *tab) { tb.appendEntry(entry{role: roleTool, text: "read"}) },
	} {
		tb := &tab{}
		tb.appendChunk("   ")
		closing(tb)
		tb.appendChunk("code")
		if got := tb.entries[len(tb.entries)-1].text; got != "code" {
			t.Fatalf("%s must drop the pending blanks, got %q", name, got)
		}
	}
}

func TestPendingBlankIsDroppedByReset(t *testing.T) {
	m := newTestModel()
	tb := m.ensureTab("a")
	m.renderInto(tb, contracts.Event{T: "chunk", Text: "   "})
	m.renderInto(tb, contracts.Event{T: "reset"})
	m.renderInto(tb, contracts.Event{T: "chunk", Text: "code"})
	if got := tb.entries[len(tb.entries)-1].text; got != "code" {
		t.Fatalf("a reset must drop the pending blanks, got %q", got)
	}
}

func TestResetLeavesTheAbandonedAttemptClosed(t *testing.T) {
	m := newTestModel()
	tb := m.ensureTab("a")
	m.renderInto(tb, contracts.Event{T: "chunk", Text: "Je regarde."})
	m.renderInto(tb, contracts.Event{T: "reset"})
	m.renderInto(tb, contracts.Event{T: "chunk", Text: "Voici"})
	var agents []entry
	for _, item := range tb.entries {
		if item.role == roleAgent {
			agents = append(agents, item)
		}
	}
	if len(agents) != 2 {
		t.Fatalf("the retry must open its own entry after the reset: %d agent entries", len(agents))
	}
	if agents[0].text != "Je regarde." || agents[0].streaming {
		t.Fatalf("the abandoned attempt must stay as it was, closed: %+v", agents[0])
	}
	if agents[1].text != "Voici" {
		t.Fatalf("the retry must hold only its own text: %q", agents[1].text)
	}
}

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
