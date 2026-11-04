package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func queueFixture(t *testing.T) (*model, *fakeBackend) {
	t.Helper()
	f := &fakeBackend{sessions: []contracts.SessionInfo{{ChannelID: "a", Name: "s1"}}}
	m := newModel(f)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.ensureTab("a")
	m.active = "a"
	return m, f
}

func typeLine(m *model, text string) {
	m.input.SetValue(text)
}

func TestEnterWhileBusyQueuesUntilTurnEnds(t *testing.T) {
	m, f := queueFixture(t)
	m.tabs["a"].busy = true
	typeLine(m, "premier")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.tabs["a"].queue) != 1 || m.tabs["a"].queue[0].steer {
		t.Fatalf("enter while busy must queue an end-of-turn item: %+v", m.tabs["a"].queue)
	}
	if len(f.submitted) != 0 {
		t.Fatal("a queued item must not be sent during the turn")
	}
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "reply", Text: "fini", Done: true}})
	if len(f.submitted) != 1 || f.submitted[0].text != "premier" {
		t.Fatalf("queued item must be sent when the turn ends: %+v", f.submitted)
	}
}

func TestAltEnterSteersAfterNextTool(t *testing.T) {
	m, f := queueFixture(t)
	m.tabs["a"].busy = true
	typeLine(m, "réoriente")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if len(m.tabs["a"].queue) != 1 || !m.tabs["a"].queue[0].steer {
		t.Fatalf("alt+enter must queue a steer item: %+v", m.tabs["a"].queue)
	}
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "status", Text: "bash"}})
	if len(f.submitted) != 1 || f.submitted[0].text != "réoriente" {
		t.Fatalf("steer item must be sent after the next tool: %+v", f.submitted)
	}
	if len(m.tabs["a"].queue) != 0 {
		t.Fatal("sent steer item must leave the queue")
	}
}

func TestEscInterruptsAndKeepsQueue(t *testing.T) {
	m, f := queueFixture(t)
	m.tabs["a"].busy = true
	m.tabs["a"].queue = []queuedInput{{text: "après"}}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if len(f.interrupted) != 1 || f.interrupted[0] != "s1" {
		t.Fatalf("esc while busy must interrupt the session: %+v", f.interrupted)
	}
	if len(m.tabs["a"].queue) != 1 {
		t.Fatal("esc must keep the queue")
	}
}

func TestCtrlEnterSendsQueueNowWhenIdle(t *testing.T) {
	m, f := queueFixture(t)
	m.tabs["a"].queue = []queuedInput{{text: "un"}, {text: "deux"}}
	m.Update(tea.KeyMsg{Type: tea.KeyF12, Alt: true})
	if len(f.submitted) != 1 || f.submitted[0].text != "un" {
		t.Fatalf("ctrl+enter must send the first queued item: %+v", f.submitted)
	}
	if len(m.tabs["a"].queue) != 1 {
		t.Fatalf("only the sent item leaves the queue: %+v", m.tabs["a"].queue)
	}
}

func TestCtrlEnterWhileBusyInterrupts(t *testing.T) {
	m, f := queueFixture(t)
	m.tabs["a"].busy = true
	m.tabs["a"].queue = []queuedInput{{text: "après"}}
	m.Update(tea.KeyMsg{Type: tea.KeyF12, Alt: true})
	if len(f.interrupted) != 1 {
		t.Fatalf("ctrl+enter while busy must interrupt so the queue goes out: %+v", f.interrupted)
	}
}

func TestLeaderEnterSendsQueue(t *testing.T) {
	m, f := queueFixture(t)
	m.tabs["a"].queue = []queuedInput{{text: "via leader"}}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(f.submitted) != 1 || f.submitted[0].text != "via leader" {
		t.Fatalf("ctrl+x enter must send the queue: %+v", f.submitted)
	}
}

func TestUpFocusesQueueAndDeleteRemovesRow(t *testing.T) {
	m, _ := queueFixture(t)
	m.tabs["a"].busy = true
	m.tabs["a"].queue = []queuedInput{{text: "un"}, {text: "deux"}}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if !m.queueFocus || m.queueSel != 1 {
		t.Fatalf("up on an empty composer must focus the last queued row: focus=%v sel=%d", m.queueFocus, m.queueSel)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDelete})
	if len(m.tabs["a"].queue) != 1 || m.tabs["a"].queue[0].text != "un" {
		t.Fatalf("suppr must remove the focused row: %+v", m.tabs["a"].queue)
	}
}

func TestEscLeavesQueueFocus(t *testing.T) {
	m, _ := queueFixture(t)
	m.tabs["a"].queue = []queuedInput{{text: "un"}}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.queueFocus {
		t.Fatal("esc must leave the queue focus")
	}
	if len(m.tabs["a"].queue) != 1 {
		t.Fatal("leaving focus must not remove anything")
	}
}

func TestMouseClickOnRemoveRemovesRow(t *testing.T) {
	m, _ := queueFixture(t)
	m.tabs["a"].busy = true
	m.tabs["a"].queue = []queuedInput{{text: "un"}, {text: "deux"}}
	m.View()
	if len(m.queueHits) != 2 {
		t.Fatalf("queue rows must register hit zones: %+v", m.queueHits)
	}
	hit := m.queueHits[0]
	m.Update(tea.MouseMsg{X: hit.from, Y: hit.row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if len(m.tabs["a"].queue) != 1 || m.tabs["a"].queue[0].text != "deux" {
		t.Fatalf("click on ✕ must remove its row: %+v", m.tabs["a"].queue)
	}
}

func TestWheelOutsideRemoveDoesNotRemove(t *testing.T) {
	m, _ := queueFixture(t)
	m.tabs["a"].busy = true
	m.tabs["a"].queue = []queuedInput{{text: "un"}}
	m.View()
	hit := m.queueHits[0]
	m.Update(tea.MouseMsg{X: hit.from, Y: hit.row, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if len(m.tabs["a"].queue) != 1 {
		t.Fatal("a wheel event on a queue row must not remove it")
	}
}

func TestQueueCommandShowsQueueAsNotice(t *testing.T) {
	m, _ := queueFixture(t)
	m.tabs["a"].queue = []queuedInput{{text: "un"}, {text: "deux"}}
	m.showQueue()
	if got := tabText(m.tabs["a"]); !strings.Contains(got, "file (2)") || !strings.Contains(got, "2. deux") {
		t.Fatalf("/queue must list the queue: %q", got)
	}
}

func TestStashDraftPushesAndPops(t *testing.T) {
	m, _ := queueFixture(t)
	typeLine(m, "brouillon")
	m.stashDraft()
	if m.input.Value() != "" || len(m.drafts) != 1 {
		t.Fatalf("ctrl+s must park the draft: input=%q drafts=%v", m.input.Value(), m.drafts)
	}
	m.stashDraft()
	if m.input.Value() != "brouillon" || len(m.drafts) != 0 {
		t.Fatalf("ctrl+s on an empty composer must bring the draft back: input=%q drafts=%v", m.input.Value(), m.drafts)
	}
}

func TestDraftsViewListsParkedDrafts(t *testing.T) {
	m, _ := queueFixture(t)
	if m.draftsView() != "" {
		t.Fatal("no drafts, no line")
	}
	typeLine(m, "à plus tard")
	m.stashDraft()
	if got := m.draftsView(); !strings.Contains(got, "brouillons (1)") {
		t.Fatalf("draft line missing: %q", got)
	}
}
