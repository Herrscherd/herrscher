package tui

import (
	"strings"
	"testing"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

const (
	rawSGR     = "\x1b[31mred\x1b[0m"
	rawOSC     = "\x1b]0;pwn\a"
	rawRLO     = "\u202e"
	escapedSGR = `\x1b[31mred`
	escapedOSC = `\x1b]0;pwn\a`
	escapedRLO = `\u202e`
)

func rawTextModel(t *testing.T) (*model, *tab) {
	t.Helper()
	m, _ := startModel(&fakeBackend{})
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	tb := m.ensureTab("c")
	m.active = "c"
	return m, tb
}

func lastEntryOf(t *testing.T, tb *tab) entry {
	t.Helper()
	if len(tb.entries) == 0 {
		t.Fatal("no entry rendered")
	}
	return tb.entries[len(tb.entries)-1]
}

func isRawControl(char rune) bool {
	if char == '\n' || char == '\t' {
		return false
	}
	return unicode.IsControl(char) || (char >= '\u202a' && char <= '\u202e') || (char >= '\u2066' && char <= '\u2069')
}

func assertNoRawControl(t *testing.T, text string) {
	t.Helper()
	for _, char := range text {
		if isRawControl(char) {
			t.Fatalf("text carries raw control %U: %q", char, text)
		}
	}
}

func assertEscapedNotRaw(t *testing.T, out, raw, escaped string) {
	t.Helper()
	if strings.Contains(out, raw) {
		t.Fatalf("output carries the raw %q: %q", raw, out)
	}
	if !strings.Contains(out, escaped) {
		t.Errorf("output does not show %q: %q", escaped, out)
	}
}

func TestStatusEventEscapesItsText(t *testing.T) {
	m, tb := rawTextModel(t)
	m.renderInto(tb, contracts.Event{T: eventwire.EventStatus, Text: "Bash " + rawSGR})
	got := lastEntryOf(t, tb).text
	assertNoRawControl(t, got)
	if !strings.Contains(got, escapedSGR) {
		t.Errorf("status does not show ESC as an escape: %q", got)
	}
}

func TestHostErrorStatusEscapesItsText(t *testing.T) {
	m, tb := rawTextModel(t)
	m.renderInto(tb, contracts.Event{T: eventwire.EventStatus, Text: hostErrPrefix + "boom " + rawRLO + "x"})
	last := lastEntryOf(t, tb)
	if last.role != roleError {
		t.Fatalf("role = %q, want %q", last.role, roleError)
	}
	assertNoRawControl(t, last.text)
	if !strings.Contains(last.text, escapedRLO) {
		t.Errorf("error does not show RLO as an escape: %q", last.text)
	}
}

func TestChunkEscapesTheStreamedText(t *testing.T) {
	m, tb := rawTextModel(t)
	m.renderInto(tb, contracts.Event{T: eventwire.EventChunk, Text: "Voici " + rawOSC})
	m.renderInto(tb, contracts.Event{T: eventwire.EventChunk, Text: rawRLO + "ligne\nsuite"})
	got := lastEntryOf(t, tb).text
	if want := `Voici \x1b]0;pwn\a` + escapedRLO + "ligne\nsuite"; got != want {
		t.Errorf("streamed text = %q, want %q", got, want)
	}
}

func TestThinkingEscapesItsText(t *testing.T) {
	m, tb := rawTextModel(t)
	m.renderInto(tb, contracts.Event{T: eventwire.EventThinking, Text: "pense " + rawSGR})
	m.renderInto(tb, contracts.Event{T: eventwire.EventThinking, Text: " " + rawOSC})
	got := lastEntryOf(t, tb)
	if got.role != roleThinking {
		t.Fatalf("role = %q, want %q", got.role, roleThinking)
	}
	assertNoRawControl(t, got.text)
	if !strings.Contains(got.text, escapedOSC) {
		t.Errorf("thinking does not show OSC as an escape: %q", got.text)
	}
}

func TestReplyEscapesTheAgentText(t *testing.T) {
	m, tb := rawTextModel(t)
	m.renderInto(tb, contracts.Event{T: eventwire.EventReply, Text: "Réponse " + rawOSC + "\nligne", Done: true})
	got := lastEntryOf(t, tb)
	if got.role != roleAgent {
		t.Fatalf("role = %q, want %q", got.role, roleAgent)
	}
	if want := `Réponse \x1b]0;pwn\a` + "\nligne"; got.text != want {
		t.Errorf("reply = %q, want %q", got.text, want)
	}
}

func TestReplyErrorEscapesItsText(t *testing.T) {
	m, tb := rawTextModel(t)
	m.renderInto(tb, contracts.Event{T: eventwire.EventReply, Text: hostErrPrefix + "panne " + rawRLO, Done: true})
	got := lastEntryOf(t, tb)
	if got.role != roleError {
		t.Fatalf("role = %q, want %q", got.role, roleError)
	}
	assertNoRawControl(t, got.text)
}

func TestScrollbackEscapesEveryReplayedLine(t *testing.T) {
	m, tb := rawTextModel(t)
	m.seedScrollback(tb, []contracts.ScrollbackLine{
		{Role: "assistant", Text: "ancien " + rawSGR},
		{Role: "user", Text: "question" + rawRLO},
	})
	if len(tb.entries) == 0 {
		t.Fatal("scrollback rendered no entry")
	}
	for _, replayed := range tb.entries {
		assertNoRawControl(t, replayed.text)
	}
}

func TestNoticeEventEscapesItsText(t *testing.T) {
	m, tb := rawTextModel(t)
	m.renderInto(tb, contracts.Event{T: eventwire.EventNotice, Text: "notice " + rawSGR})
	got := lastEntryOf(t, tb)
	if got.role != roleNotice {
		t.Fatalf("role = %q, want %q", got.role, roleNotice)
	}
	assertNoRawControl(t, got.text)
}

func TestOrdinaryTextStaysReadable(t *testing.T) {
	m, tb := rawTextModel(t)
	reply := "Voici l'état : déjà ✓\n\n\tcode ●"
	m.renderInto(tb, contracts.Event{T: eventwire.EventReply, Text: reply, Done: true})
	if got := lastEntryOf(t, tb).text; got != reply {
		t.Errorf("reply = %q, want %q", got, reply)
	}
	notice := "démon ✕\nligne deux"
	m.renderInto(tb, contracts.Event{T: eventwire.EventNotice, Text: notice})
	if got := lastEntryOf(t, tb).text; got != notice {
		t.Errorf("notice = %q, want %q", got, notice)
	}
}

func TestFlashEscapesItsText(t *testing.T) {
	m, _ := rawTextModel(t)
	m.flash = "échec " + rawOSC
	assertEscapedNotRaw(t, m.statusLeft(200), rawOSC, escapedOSC)
}

func TestOneLineEscapesBidiAndStripsSequences(t *testing.T) {
	if got := oneLine("a" + rawRLO + "b"); got != "a"+escapedRLO+"b" {
		t.Errorf("oneLine bidi = %q, want %q", got, "a"+escapedRLO+"b")
	}
	if got := oneLine(rawSGR); got != "red" {
		t.Errorf("oneLine sequence = %q, want %q", got, "red")
	}
}

func TestApprovalCommandEscapesToolCommandAndStatement(t *testing.T) {
	m, _ := startModel(&fakeBackend{})
	assertEscapedNotRaw(t, m.approvalCommand(approvalPrompt{tool: "Bash", command: "ls" + rawOSC}, 200), rawOSC, escapedOSC)
	assertEscapedNotRaw(t, m.approvalCommand(approvalPrompt{statement: "Autoriser" + rawRLO}, 200), rawRLO, escapedRLO)
}

func TestDiffRowEscapesItsText(t *testing.T) {
	m, _ := startModel(&fakeBackend{})
	assertEscapedNotRaw(t, m.diffRowLine(gitRow{Kind: rowAdded, Text: "x" + rawOSC}, 120, false), rawOSC, escapedOSC)
}

func TestAttachmentChipEscapesItsName(t *testing.T) {
	assertEscapedNotRaw(t, chipRow([]Attachment{{Name: "a" + rawOSC, Size: 10}}, testStyles()), rawOSC, escapedOSC)
}

func TestResumePickerEscapesProjectTimeAndVendor(t *testing.T) {
	m, _ := startModel(&fakeBackend{})
	m.resumeRows = []contracts.SessionInfo{{Name: "s", Project: "p" + rawOSC, LastTs: "t" + rawOSC, Vendor: "v" + rawOSC}}
	m.resumeIdx = 0
	assertEscapedNotRaw(t, m.resumeView(), rawOSC, escapedOSC)
}

func TestSwitchPickerEscapesNameAndProject(t *testing.T) {
	m, _ := startModel(&fakeBackend{})
	m.switchRows = []contracts.SessionInfo{{Name: "s" + rawOSC, Project: "p" + rawOSC}}
	m.switchIdx = 0
	assertEscapedNotRaw(t, m.switchView(), rawOSC, escapedOSC)
}

func TestResumePreviewEscapesScrollbackLines(t *testing.T) {
	backend := &fakeBackend{scrollback: map[string][]contracts.ScrollbackLine{"s": {{Role: "user", Text: "question" + rawOSC}}}}
	m, _ := startModel(backend)
	m.resumeRows = []contracts.SessionInfo{{Name: "s"}}
	m.resumeIdx = 0
	m.loadResumePreview()
	if len(m.resumePreviewLines) != 1 {
		t.Fatalf("preview lines = %q, want one line", m.resumePreviewLines)
	}
	assertEscapedNotRaw(t, m.resumePreviewLines[0], rawOSC, escapedOSC)
}

func TestRewindListEscapesTurnText(t *testing.T) {
	m, _ := startModel(&fakeBackend{})
	m.rwRows = []rewindRow{{Index: 1, At: "10:00", Text: "turn" + rawOSC}}
	m.rwIdx = 0
	assertEscapedNotRaw(t, strings.Join(m.rewindListLines(120), "\n"), rawOSC, escapedOSC)
}

func TestApprovalNoteEscapesItsText(t *testing.T) {
	m, _ := startModel(&fakeBackend{})
	choice := approvalCatalog()[0]
	choice.note = "note" + rawOSC
	assertEscapedNotRaw(t, strings.Join(m.approvalChoiceLines(choice, false, true, true), "\n"), rawOSC, escapedOSC)
}

func TestApprovalReasonEscapesItsText(t *testing.T) {
	m, _ := startModel(&fakeBackend{})
	assertEscapedNotRaw(t, m.approvalReasonLine(approvalPrompt{reasonOpen: true, reason: "raison" + rawOSC}), rawOSC, escapedOSC)
}

func TestToastEscapesTitleAndBodyBeforeSignalling(t *testing.T) {
	m, _ := startModel(&fakeBackend{})
	m.announce(kindTurnEnd, "ok", "titre\u009b"+rawRLO, "chan", []string{"corps" + rawRLO + "\u009b"})
	if m.toast.title != `titre\u009b`+escapedRLO {
		t.Errorf("toast title = %q, want the escaped form", m.toast.title)
	}
	if len(m.toast.body) != 1 || strings.ContainsAny(m.toast.body[0], "\u009b\u202e") {
		t.Errorf("toast body = %q, want no raw C1 or bidi control", m.toast.body)
	}
}

func TestCopyReplyYieldsTheOriginalAgentText(t *testing.T) {
	m, tb := rawTextModel(t)
	tb.appendEntry(entry{role: roleAgent, text: rawSGR + "\n\tsuite"})
	if got := copyText(m.tr, tb.entries, copyReply); got != rawSGR+"\n\tsuite" {
		t.Errorf("copy reply = %q, want the original agent text", got)
	}
}

func TestCopyTurnYieldsTheOriginalText(t *testing.T) {
	m, tb := rawTextModel(t)
	tb.appendEntry(entry{role: roleYou, text: "question " + rawOSC})
	tb.appendEntry(entry{role: roleAgent, text: rawSGR})
	got := copyText(m.tr, tb.entries, copyTurn)
	if !strings.Contains(got, "question "+rawOSC) || !strings.Contains(got, rawSGR) {
		t.Errorf("copy turn = %q, want the original texts", got)
	}
}

func TestAgentTabIsExpandedStablyAndNeverCarriesARawTab(t *testing.T) {
	m, tb := rawTextModel(t)
	tb.appendEntry(entry{role: roleAgent, text: "a\tb"})
	item := lastEntryOf(t, tb)
	first := renderEntry(item, 80, m.displayMode())
	if strings.Contains(first, "\t") {
		t.Fatalf("rendered reply carries a raw tab: %q", first)
	}
	if second := renderEntry(item, 80, m.displayMode()); second != first {
		t.Errorf("rendering is not stable: %q then %q", first, second)
	}
}

func renderedAgentText(m *model, text string, streaming bool) string {
	return renderEntry(entry{role: roleAgent, text: text, streaming: streaming}, 80, m.displayMode())
}

func TestAgentTabAlignsOnTheNextFourColumnStop(t *testing.T) {
	m, _ := rawTextModel(t)
	cases := []struct{ withTab, withSpaces string }{
		{"a\tb", "a   b"},
		{"abc\td", "abc d"},
		{"abcd\te", "abcd    e"},
		{"文字\tb", "文字    b"},
		{"\U0001F44D\U0001F3FD\tx", "\U0001F44D\U0001F3FD  x"},
		{"\U0001F469‍\U0001F4BB\tx", "\U0001F469‍\U0001F4BB  x"},
	}
	for _, streaming := range []bool{false, true} {
		for _, tc := range cases {
			got := renderedAgentText(m, tc.withTab, streaming)
			want := renderedAgentText(m, tc.withSpaces, streaming)
			if got != want {
				t.Errorf("streaming=%v: %q rendered %q, want the stop-aligned %q", streaming, tc.withTab, got, want)
			}
		}
	}
}

func TestAgentTabIndentedCodeBlockStaysACodeBlock(t *testing.T) {
	m, _ := rawTextModel(t)
	withTab := renderedAgentText(m, "para\n\n\tcode", false)
	withSpaces := renderedAgentText(m, "para\n\n    code", false)
	if withTab != withSpaces {
		t.Errorf("tab-indented block rendered %q, want the four-column code block %q", withTab, withSpaces)
	}
}
