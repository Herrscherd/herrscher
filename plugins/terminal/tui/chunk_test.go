package tui

import (
	"runtime"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

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
		{"space and newline", []string{" \n", "code"}, "code"},
		{"ideographic space and newline", []string{"\u3000\n", "code"}, "code"},
		{"form feed and newline", []string{"\f\n", "code"}, "code"},
		{"vertical tab and newline", []string{"\v\n", "code"}, "code"},
		{"next line and newline", []string{"\u0085\n", "code"}, "code"},
		{"carriage return then indent", []string{"\r   ", "code"}, "   code"},
		{"carriage return and newline then indent", []string{"\r\n   ", "code"}, "   code"},
		{"next line alone then indent", []string{"\u0085   code"}, "   code"},
		{"form feed alone then indent", []string{"\f   code"}, "   code"},
		{"vertical tab alone then indent", []string{"\v   code"}, "   code"},
		{"line separator alone then indent", []string{"\u2028   code"}, "   code"},
		{"paragraph separator alone then indent", []string{"\u2029   code"}, "   code"},
		{"next line fragment then indent", []string{"\u0085", "   ", "code"}, "   code"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, split := range []struct {
				label  string
				chunks []string
			}{
				{"fragments", tc.chunks},
				{"joined", []string{strings.Join(tc.chunks, "")}},
			} {
				t.Run(split.label, func(t *testing.T) {
					tb := openedFrom(split.chunks)
					if len(tb.entries) != 1 {
						t.Errorf("the next non-empty chunk must open one entry from %q: %d entries", split.chunks, len(tb.entries))
						return
					}
					if got := tb.entries[0].text; got != tc.want || !tb.entries[0].streaming {
						t.Errorf("the entry from %q must open on %q: %+v", split.chunks, tc.want, tb.entries[0])
					}
				})
			}
		})
	}
}

func TestBlankFragmentsAreNotRecopiedPerChunk(t *testing.T) {
	tb := &tab{}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 40000 {
		tb.appendChunk("\n")
	}
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
		t.Fatalf("40000 blank fragments must not recopy their pending blanks: %d bytes allocated", allocated)
	}
	tb.appendChunk("code")
	if len(tb.entries) != 1 || tb.entries[0].text != "code" {
		t.Fatalf("the blank fragments must open the entry on the next text alone: %+v", tb.entries)
	}
}

func TestBlankStreamDoesNotHideTheFinalReply(t *testing.T) {
	m := newTestModel()
	tb := m.ensureTab("a")
	m.renderInto(tb, contracts.Event{T: eventwire.EventChunk, Text: "\n\n"})
	m.renderInto(tb, contracts.Event{T: eventwire.EventReply, Text: "Réponse finale", Done: true})
	for _, item := range tb.entries {
		if item.role == roleAgent && item.text == "Réponse finale" {
			return
		}
	}
	t.Fatalf("a turn whose stream held only blank lines must still show its final reply: %+v", tb.entries)
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
	m.renderInto(tb, contracts.Event{T: eventwire.EventChunk, Text: "   "})
	m.renderInto(tb, contracts.Event{T: eventwire.EventReset})
	m.renderInto(tb, contracts.Event{T: eventwire.EventChunk, Text: "code"})
	if got := tb.entries[len(tb.entries)-1].text; got != "code" {
		t.Fatalf("a reset must drop the pending blanks, got %q", got)
	}
}
