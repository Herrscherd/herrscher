package tui

import (
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	tea "github.com/charmbracelet/bubbletea"
)

type blockingHistory struct {
	*fakeBackend
	release chan struct{}
}

func (b *blockingHistory) Scrollback(name string) ([]contracts.ScrollbackLine, error) {
	<-b.release
	return b.fakeBackend.Scrollback(name)
}

func historyMsgs(cmd tea.Cmd) []scrollbackMsg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var out []scrollbackMsg
		for _, child := range msg {
			out = append(out, historyMsgs(child)...)
		}
		return out
	case scrollbackMsg:
		return []scrollbackMsg{msg}
	}
	return nil
}

func loadHistory(m *model) {
	for _, msg := range historyMsgs(m.drainHistory()) {
		m.Update(msg)
	}
}

func syncLoaded(m *model) {
	settleAll(m, m.syncTabs())
	loadHistory(m)
}

func TestNewSessionAtTheTickOpensItsTabWithoutReadingHistory(t *testing.T) {
	fresh := contracts.SessionInfo{Name: "fresh", ChannelID: "c2"}
	f := &blockingHistory{
		fakeBackend: &fakeBackend{fe: make(chan RoutedEvent, 1), sessions: []contracts.SessionInfo{fresh}},
		release:     make(chan struct{}),
	}
	m := newModel(f)
	returnsWithin(t, "the tick waited on the daemon history", func() {
		m.Update(tickMsg{})
		m.Update(sessionsMsg{seq: m.sessionsIssued, infos: f.sessions})
	})
	tb := m.tabs["c2"]
	if tb == nil {
		t.Fatal("the tick did not open the tab of the new session")
	}
	if tb.label != "fresh" {
		t.Errorf("tab label = %q, want %q", tb.label, "fresh")
	}
	if !tb.awaitingHistory() {
		t.Fatalf("tab history = %v, want a history still queued or requested", tb.history)
	}
	close(f.release)
	loadHistory(m)
	if tb.history != historyDone {
		t.Errorf("tab history = %v after the load, want historyDone", tb.history)
	}
}

func TestLateHistoryNeverFillsAClosedOrReopenedTab(t *testing.T) {
	f := &fakeBackend{
		fe:       make(chan RoutedEvent, 1),
		sessions: []contracts.SessionInfo{{Name: "s", ChannelID: "c"}},
		scrollback: map[string][]contracts.ScrollbackLine{"s": {
			{Role: "user", Text: "old"},
		}},
	}
	m := newModel(f)
	m.Update(sessionsMsg{seq: m.sessionsIssued, infos: f.sessions})
	stale := historyMsgs(m.drainHistory())

	m.removeTab("c")
	for _, msg := range stale {
		m.Update(msg)
	}
	if _, ok := m.tabs["c"]; ok {
		t.Fatal("late history reopened the closed tab")
	}

	m.ensureTabNamed("c", "s")
	fresh := historyMsgs(m.drainHistory())
	for _, msg := range stale {
		m.Update(msg)
	}
	reopened := m.tabs["c"]
	if reopened == nil {
		t.Fatal("reopened tab missing")
	}
	if len(reopened.entries) != 0 {
		t.Fatalf("stale history from the previous open filled the reopened tab: %q", tabText(reopened))
	}
	if reopened.history != historyRequested {
		t.Fatalf("reopened tab history = %v, want historyRequested", reopened.history)
	}
	for _, msg := range fresh {
		m.Update(msg)
	}
	if !strings.Contains(tabText(reopened), "old") {
		t.Errorf("reopened tab lost its own history: %q", tabText(reopened))
	}
}

func TestLiveEventsWaitForHistoryAndKeepTheirOrder(t *testing.T) {
	f := &fakeBackend{
		fe:         make(chan RoutedEvent, 1),
		sessions:   []contracts.SessionInfo{{Name: "s", ChannelID: "c"}},
		scrollback: map[string][]contracts.ScrollbackLine{"s": {{Role: "user", Text: "history-line"}}},
	}
	m := newModel(f)
	m.Update(sessionsMsg{seq: m.sessionsIssued, infos: f.sessions})
	live := func(text string) RoutedEvent {
		return RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventChunk, Text: text}}
	}
	historyCmd := m.route(live("live-first"))
	m.route(live("live-second"))
	tb := m.tabs["c"]
	if strings.Contains(tabText(tb), "live-first") {
		t.Fatalf("live event applied before the history arrived: %q", tabText(tb))
	}
	for _, msg := range historyMsgs(historyCmd) {
		m.Update(msg)
	}
	text := tabText(tb)
	history := strings.Index(text, "history-line")
	first := strings.Index(text, "live-first")
	second := strings.Index(text, "live-second")
	if history < 0 || first < 0 || second < 0 {
		t.Fatalf("missing lines in tab: %q", text)
	}
	if !(history < first && first < second) {
		t.Errorf("tab order = history %d, live-first %d, live-second %d; want increasing", history, first, second)
	}
}

func TestHistoryErrorIsShownAndTheTabStaysUsable(t *testing.T) {
	failure := errors.New("daemon down")
	f := &fakeBackend{
		fe:            make(chan RoutedEvent, 1),
		sessions:      []contracts.SessionInfo{{Name: "s", ChannelID: "c"}},
		scrollbackErr: failure,
	}
	m := newModel(f)
	m.Update(sessionsMsg{seq: m.sessionsIssued, infos: f.sessions})
	loadHistory(m)
	tb := m.tabs["c"]
	want := m.tr.t("tui.session.history_failed", map[string]any{tplArgReason: localizedReason(m.tr, failure)})
	if len(tb.entries) == 0 {
		t.Fatal("history failure left no entry in the tab")
	}
	last := tb.entries[len(tb.entries)-1]
	if last.role != roleError || last.text != want {
		t.Fatalf("last entry = (%q, %q), want (%q, %q)", last.role, last.text, roleError, want)
	}
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: eventwire.EventChunk, Text: "still-live"}})
	if tb.history != historyDone {
		t.Errorf("tab history = %v after the failure, want historyDone", tb.history)
	}
	if !strings.Contains(tabText(tb), "still-live") {
		t.Errorf("tab unusable after the history failure: %q", tabText(tb))
	}
}
