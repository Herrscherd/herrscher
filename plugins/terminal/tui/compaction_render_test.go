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

	m.renderInto(tb, contracts.Event{T: eventCompactionStarted, Value: "automatic", Tokens: 184_000, TokensIn: 200_000, Done: true})
	if !tb.busy || !tb.compaction.active {
		t.Fatal("a started compaction must mark the tab busy and active")
	}

	m.renderInto(tb, contracts.Event{T: eventCompactionDone, Value: "automatic", Text: "résumé", Tokens: 90_000, TokensIn: 200_000, Done: true})
	if tb.busy || tb.compaction.active {
		t.Fatal("a finished compaction must release the tab")
	}
	if tb.ctxTokens != 90_000 {
		t.Fatalf("ctxTokens = %d, want the measured 90000 after compaction", tb.ctxTokens)
	}

	m.renderInto(tb, contracts.Event{T: eventCompactionStarted, Value: "manual", Tokens: 90_000, TokensIn: 200_000, Done: true})
	m.renderInto(tb, contracts.Event{T: eventCompactionFailed, Value: "manual", Text: "le backend a refusé"})
	if tb.busy {
		t.Fatal("a failed compaction must release the tab")
	}
	last := tb.entries[len(tb.entries)-1]
	if last.role != roleError || last.text != "le backend a refusé" {
		t.Fatalf("last entry = %+v, want the failure as an error", last)
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
