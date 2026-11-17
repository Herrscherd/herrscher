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
