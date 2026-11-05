package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestCompactionEventsDriveTheTabFromStartToNotice(t *testing.T) {
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude", AutoCompactPercent: 92}}}
	m := newModel(f)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	tb := m.ensureTab("c")
	m.active = "c"

	m.renderInto(tb, contracts.Event{T: eventCompactionStarted, Value: "automatic", Tokens: 184_000})
	if !tb.busy || !tb.compaction.active {
		t.Fatal("a started compaction must mark the tab busy and active")
	}

	m.renderInto(tb, contracts.Event{T: eventCompactionDone, Value: "automatic", Text: "résumé", Tokens: 90_000})
	if tb.busy || tb.compaction.active {
		t.Fatal("a finished compaction must release the tab")
	}
	if tb.ctxTokens != 90_000 {
		t.Fatalf("ctxTokens = %d, want the measured 90000 after compaction", tb.ctxTokens)
	}

	m.renderInto(tb, contracts.Event{T: eventCompactionStarted, Value: "manual", Tokens: 90_000})
	m.renderInto(tb, contracts.Event{T: eventCompactionFailed, Value: "manual", Text: "le backend a refusé"})
	if tb.busy {
		t.Fatal("a failed compaction must release the tab")
	}
	last := tb.entries[len(tb.entries)-1]
	if last.role != roleError || last.text != "le backend a refusé" {
		t.Fatalf("last entry = %+v, want the failure as an error", last)
	}
}

func TestCompactionNoticeShowsTheFiguresOnlyWhenMeasured(t *testing.T) {
	cases := []struct {
		name          string
		started, done int
		want          string
	}{
		{name: "measured both ends", started: 184_000, done: 90_000, want: formatTokens(184_000) + arrow + formatTokens(90_000)},
		{name: "no measured turn and no measured result", started: 0, done: 0, want: formatTokens(0) + arrow + unavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude", AutoCompactPercent: 92}}}
			m := newModel(f)
			m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
			tb := m.ensureTab("c")
			m.active = "c"
			m.renderInto(tb, contracts.Event{T: eventCompactionStarted, Value: "automatic", Tokens: tc.started})
			m.renderInto(tb, contracts.Event{T: eventCompactionDone, Value: "automatic", Tokens: tc.done})
			last := tb.entries[len(tb.entries)-1]
			if last.role != roleNotice || last.text != "compacté : "+tc.want {
				t.Fatalf("notice = %+v, want %q", last, "compacté : "+tc.want)
			}
		})
	}
}

func TestCompactionEventsDoNotOverwriteTheTokenCounter(t *testing.T) {
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude", AutoCompactPercent: 92}}}
	m := newModel(f)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	tb := m.ensureTab("c")
	m.active = "c"
	tb.tokens = 1234
	m.renderInto(tb, contracts.Event{T: eventCompactionStarted, Value: "automatic", Tokens: 184_000})
	m.renderInto(tb, contracts.Event{T: eventCompactionDone, Value: "automatic", Tokens: 90_000})
	if tb.tokens != 1234 {
		t.Fatalf("tokens = %d after a compaction, want the thinking and tool counter kept at 1234", tb.tokens)
	}
}

func TestUnmeasuredCompactionReturnsTheGaugeToItsEstimate(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 60, Height: 20}, {Width: 80, Height: 30}} {
		f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude", ContextTokens: 42_000, AutoCompactPercent: 92}}}
		m := newModel(f)
		m.handleWindowSize(size)
		tb := m.ensureTab("c")
		m.active = "c"
		tb.measureContext(contracts.Event{TokensIn: 150_000})
		m.renderInto(tb, contracts.Event{T: eventCompactionStarted, Value: "manual"})
		m.renderInto(tb, contracts.Event{T: eventCompactionDone, Value: "manual", Text: "compacté"})
		if tb.ctxMeasured || tb.ctxTokens != 0 {
			t.Fatalf("%dx%d: gauge still holds the pre-compaction reading: measured %t, tokens %d", size.Width, size.Height, tb.ctxMeasured, tb.ctxTokens)
		}
		if got := m.contextTokens(tb); got != 42_000 {
			t.Fatalf("%dx%d: gauge = %d, want the estimate 42000", size.Width, size.Height, got)
		}
		m.openContext()
		view := ansi.Strip(m.contextView())
		if strings.Contains(view, formatTokens(150_000)) {
			t.Errorf("%dx%d: context view still shows the pre-compaction 150000:\n%s", size.Width, size.Height, view)
		}
		if rows := strings.Count(view, "\n") + 1; rows > size.Height {
			t.Errorf("%dx%d: the context view takes %d rows, more than the window", size.Width, size.Height, rows)
		}
	}
}

func TestContextViewSaysNoAutoCompactionInSmallWindows(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 60, Height: 20}, {Width: 80, Height: 30}} {
		f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude", AutoCompactPercent: 0}}}
		m := newModel(f)
		m.handleWindowSize(size)
		tb := m.ensureTab("c")
		m.active = "c"
		tb.measureContext(contracts.Event{TokensIn: 82_000})
		m.openContext()
		view := ansi.Strip(m.contextView())
		if !strings.Contains(view, "pas de compaction auto") {
			t.Errorf("%dx%d: the total line must say there is no automatic compaction:\n%s", size.Width, size.Height, view)
		}
		if rows := strings.Count(view, "\n") + 1; rows > size.Height {
			t.Errorf("%dx%d: the context view takes %d rows, more than the window", size.Width, size.Height, rows)
		}
	}
}

func TestNoticeEventAddsANoticeLine(t *testing.T) {
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude", AutoCompactPercent: 92}}}
	m := newModel(f)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	tb := m.ensureTab("c")
	m.active = "c"

	m.renderInto(tb, contracts.Event{T: eventNotice, Text: "contexte compacté"})
	last := tb.entries[len(tb.entries)-1]
	if last.role != roleNotice || last.text != "contexte compacté" {
		t.Fatalf("last entry = %+v, want the notice as a notice line", last)
	}
}

func TestFailureWithoutTextAddsNoErrorLine(t *testing.T) {
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude", AutoCompactPercent: 92}}}
	m := newModel(f)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	tb := m.ensureTab("c")
	m.active = "c"

	before := len(tb.entries)
	m.renderInto(tb, contracts.Event{T: eventCompactionFailed, Value: "manual"})
	if tb.busy || len(tb.entries) != before {
		t.Fatalf("a failure without text must release the tab and add no line, entries %d -> %d", before, len(tb.entries))
	}
}
