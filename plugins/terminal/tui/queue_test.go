package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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
	settle(m, m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "reply", Text: "fini", Done: true}}))
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
	settle(m, m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "status", Text: "bash"}}))
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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF12, Alt: true})
	settle(m, cmd)
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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	settle(m, cmd)
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

func TestShiftArrowsReorderFocusedRow(t *testing.T) {
	m, _ := queueFixture(t)
	m.tabs["a"].busy = true
	m.tabs["a"].queue = []queuedInput{{text: "un"}, {text: "deux"}, {text: "trois"}}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m.Update(tea.KeyMsg{Type: tea.KeyShiftUp})
	got := m.tabs["a"].queue
	if got[0].text != "deux" || got[1].text != "un" || m.queueSel != 0 {
		t.Fatalf("shift+up must move the focused row up and follow it: %+v sel=%d", got, m.queueSel)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyShiftDown})
	if m.tabs["a"].queue[0].text != "un" || m.tabs["a"].queue[1].text != "deux" || m.queueSel != 1 {
		t.Fatalf("shift+down must move it back: %+v sel=%d", m.tabs["a"].queue, m.queueSel)
	}
}

func TestShiftUpAtTopDoesNothing(t *testing.T) {
	m, _ := queueFixture(t)
	m.tabs["a"].queue = []queuedInput{{text: "un"}, {text: "deux"}}
	m.queueFocus = true
	m.queueSel = 0
	m.Update(tea.KeyMsg{Type: tea.KeyShiftUp})
	if m.tabs["a"].queue[0].text != "un" {
		t.Fatalf("the first row cannot move up: %+v", m.tabs["a"].queue)
	}
}

func TestBusyPlaceholderMatchesMockup(t *testing.T) {
	m, _ := queueFixture(t)
	m.tabs["a"].busy = true
	m.syncComposer()
	m.View()
	if got := m.input.Placeholder; got != "Écris pendant que l’agent travaille : ⏎ met en file, ⌥⏎ réoriente" {
		t.Fatalf("busy placeholder = %q", got)
	}
	m.tabs["a"].busy = false
	m.syncComposer()
	m.View()
	if got := m.input.Placeholder; got != "Tape un message, / ou @, ou !commande" {
		t.Fatalf("idle placeholder = %q", got)
	}
}

func textColumn(t *testing.T, line, text string) int {
	t.Helper()
	index := strings.Index(line, text)
	if index < 0 {
		t.Fatalf("%q must be on the row %q", text, ansi.Strip(line))
	}
	return lipgloss.Width(line[:index])
}

func TestQueueRowsAlignTheirTextWhateverTheMark(t *testing.T) {
	m, _ := queueFixture(t)
	m.tabs["a"].busy = true
	m.tabs["a"].queue = []queuedInput{{text: "pense aussi"}, {text: "garde l’ordre", steer: true}}
	lines := m.queueLines(0, unboundedRows)
	file := textColumn(t, lines[1], "pense aussi")
	steer := textColumn(t, lines[2], "garde l’ordre")
	if file != steer {
		t.Fatalf("the file and steer rows must start their text in the same column, got %d and %d", file, steer)
	}
}

func TestQueueRowCutsLongTextWithEllipsis(t *testing.T) {
	lines := renderAt(t, 60, func(m *model) {
		m.tabs["a"].busy = true
		m.tabs["a"].queue = []queuedInput{{text: "pense aussi au mode étroit, sous 60 colonnes, la file tient"}}
		m.applySize()
		m.syncViewport()
	})
	for _, line := range lines {
		stripped := ansi.Strip(line)
		if !strings.Contains(stripped, "pense aussi au m") {
			continue
		}
		if !strings.Contains(stripped, "…") || !strings.Contains(stripped, "✕ retirer") {
			t.Fatalf("a cut queue row must end with an ellipsis and keep its remove label, got %q", stripped)
		}
		return
	}
	t.Fatal("the queued row must be visible at 60 columns")
}

func TestQueueHeaderFoldsWholeSegmentsAtSixtyColumns(t *testing.T) {
	lines := renderAt(t, 60, func(m *model) {
		m.tabs["a"].busy = true
		m.tabs["a"].queue = []queuedInput{{text: "une demande"}}
		m.applySize()
		m.syncViewport()
	})
	stripped := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(stripped, "○ En file (1) · partent dans l’ordre à la fin du tour") {
		t.Fatalf("the queue header must keep its whole first segments at 60 columns:\n%s", stripped)
	}
	if strings.Contains(stripped, "tout envoyer") {
		t.Fatal("the queue header must fold away its last segment rather than cut it")
	}
}

func queueOfSize(m *model, count int) {
	tb := m.tabs["c1"]
	tb.busy = true
	for index := range count {
		tb.queue = append(tb.queue, queuedInput{text: "message " + strconv.Itoa(index+1)})
	}
}

func TestQueueFoldsUnderTheOpenOverlay(t *testing.T) {
	m, _ := barModelAt(t, "")
	queueOfSize(m, 15)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.openStatusline()
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Barre d’état · /statusline") {
		t.Fatalf("the open overlay must keep its title ahead of the queue:\n%s", view)
	}
	if !strings.Contains(view, "En file (15)") {
		t.Fatalf("the queue header must stay visible:\n%s", view)
	}
	if strings.Contains(view, "message 15") {
		t.Fatalf("the queue must fold instead of showing every entry:\n%s", view)
	}
	if rows := len(strings.Split(view, "\n")); rows > 30 {
		t.Fatalf("the view has %d rows for a 30 row window", rows)
	}
}

func queueRowsInView(view string) (int, int) {
	shown, hidden := 0, 0
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, " ○ file ") {
			shown++
		}
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "… "); ok {
			if count, found := strings.CutSuffix(rest, " de plus"); found {
				if n, err := strconv.Atoi(count); err == nil {
					hidden = n
				}
			}
		}
	}
	return shown, hidden
}

func TestQueueFoldsWithAnOverflowLineInShortWindows(t *testing.T) {
	for _, size := range []struct{ width, height int }{{60, 20}, {80, 30}} {
		m, _ := barModelAt(t, "")
		queueOfSize(m, 20)
		m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		shown, hidden := queueRowsInView(ansi.Strip(m.View()))
		if hidden == 0 {
			t.Fatalf("%dx%d: a folded queue must say how many entries are hidden", size.width, size.height)
		}
		if shown+hidden != 20 {
			t.Fatalf("%dx%d: %d shown and %d hidden, want 20 in all", size.width, size.height, shown, hidden)
		}
	}
}

func TestQueueNumbersAlignPastNine(t *testing.T) {
	m, _ := barModelAt(t, "")
	queueOfSize(m, 12)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	var nine, ten int
	for _, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(line, "message 9 ") {
			nine = strings.Index(line, "○")
		}
		if strings.Contains(line, "message 10 ") {
			ten = strings.Index(line, "○")
		}
	}
	if nine == 0 || nine != ten {
		t.Fatalf("the mark column must not move past nine: entry 9 at %d, entry 10 at %d", nine, ten)
	}
}
