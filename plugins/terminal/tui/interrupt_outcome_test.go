package tui

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func TestAnInterruptedReplyWithoutOutputShowsTheInterruptionNotice(t *testing.T) {
	m := newTestModel()
	tb := m.ensureTab("a")
	m.renderInto(tb, contracts.Event{T: eventwire.EventReply, Done: true, Outcome: contracts.TurnInterrupted})
	want, err := contracts.NewRenderer().Render(m.tr.lang, contracts.Notice{Code: contracts.CodeTurnInterrupted})
	if err != nil || want == "" {
		t.Fatalf("interruption notice does not render: %q, %v", want, err)
	}
	if len(tb.entries) != 1 || tb.entries[0].role != roleNotice || tb.entries[0].text != want {
		t.Fatalf("entries = %+v, want exactly one interruption notice %q", tb.entries, want)
	}
}

func TestAnInterruptedReplyShowsTheNoticeAfterItsPartialOutput(t *testing.T) {
	m := newTestModel()
	tb := m.ensureTab("a")
	m.renderInto(tb, contracts.Event{T: eventwire.EventReply, Text: "Je regarde encore", Done: true, Outcome: contracts.TurnInterrupted})
	want, err := contracts.NewRenderer().Render(m.tr.lang, contracts.Notice{Code: contracts.CodeTurnInterrupted})
	if err != nil {
		t.Fatalf("interruption notice does not render: %v", err)
	}
	if len(tb.entries) != 2 || tb.entries[0].text != "Je regarde encore" || tb.entries[1].text != want {
		t.Fatalf("entries = %+v, want the partial output then the interruption notice %q", tb.entries, want)
	}
}

func TestACompletedReplyShowsNoInterruptionNotice(t *testing.T) {
	m := newTestModel()
	tb := m.ensureTab("a")
	m.renderInto(tb, contracts.Event{T: eventwire.EventReply, Text: "fini", Done: true})
	for _, item := range tb.entries {
		if item.role == roleNotice {
			t.Fatalf("a completed reply must not show an interruption notice: %+v", tb.entries)
		}
	}
}
