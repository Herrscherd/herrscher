package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestStatuslineCommandOpensTheOverlayBelowTheComposer(t *testing.T) {
	m, _ := barModelAt(t, "")
	if _, handled := m.runLocalSlash("/statusline", []string{"statusline"}); !handled {
		t.Fatal("/statusline must be a local command")
	}
	if !strings.Contains(strings.Join(m.buildFrame().below, "\n"), "/statusline") {
		t.Errorf("the overlay must show under the frame when open")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if strings.Contains(strings.Join(m.buildFrame().below, "\n"), "/statusline") {
		t.Errorf("esc must close the overlay")
	}
}

func TestUntickedFieldLeavesTheBar(t *testing.T) {
	m, tb := barModelAt(t, "")
	if !strings.Contains(m.statusBar(tb, 200), "$1.42") {
		t.Fatal("the cost must show before it is unticked")
	}
	m.openStatusline()
	ids := m.bar.fieldIDs()
	for i, id := range ids {
		if id == "cost" {
			for range i {
				m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
			}
			break
		}
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if strings.Contains(m.statusBar(tb, 200), "$1.42") {
		t.Errorf("an unticked cost must leave the bar: %q", m.statusBar(tb, 200))
	}
}

func TestStatuslineNotesStartOnTheSameColumn(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.openStatusline()
	lines := strings.Split(m.statuslineView(), "\n")
	var columns []int
	for _, note := range []string{"claude, codex", "herrscher:feat", "(worktree)", "go test -race"} {
		for _, line := range lines {
			if index := strings.Index(line, note); index >= 0 {
				columns = append(columns, lipgloss.Width(line[:index]))
				break
			}
		}
	}
	if len(columns) != 4 {
		t.Fatalf("found %d notes in the overlay, want 4", len(columns))
	}
	for _, column := range columns {
		if column != columns[0] {
			t.Errorf("notes start at columns %v, want one column", columns)
		}
	}
}

func TestStatuslinePaneSitsBesideTheListWhenWide(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.openStatusline()
	view := m.statuslineView()
	for _, want := range []string{"SCRIPT", "statusline.sh", "JSON RECEIVED", "TERMINAL.TOML"} {
		if !strings.Contains(view, want) {
			t.Errorf("the wide overlay must show %q beside the list", want)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "SCRIPT") && !strings.Contains(line, "│") {
			t.Errorf("the script pane must share a row with the list divider: %q", line)
		}
	}
}

func TestStatuslinePaneStacksUnderTheListWhenNarrow(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.openStatusline()
	lines := strings.Split(m.statuslineView(), "\n")
	footer, script := -1, -1
	for index, line := range lines {
		if strings.Contains(line, "space check") {
			footer = index
		}
		if strings.Contains(line, "SCRIPT") && script < 0 {
			script = index
		}
	}
	if script < 0 || footer < 0 || script < footer {
		t.Fatalf("the narrow overlay must stack the script pane below the list (footer %d, script %d)", footer, script)
	}
	for _, line := range lines {
		if lipgloss.Width(line) > 80 {
			t.Errorf("no row may be wider than the window: %q", line)
		}
	}
}

func TestStatuslineFitsEveryWindowSize(t *testing.T) {
	sizes := []struct{ width, height int }{{60, 20}, {80, 30}, {120, 30}, {200, 30}}
	for _, size := range sizes {
		m, _ := barModelAt(t, "")
		m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		m.openStatusline()
		lines := strings.Split(m.View(), "\n")
		if len(lines) > size.height {
			t.Errorf("%dx%d: the view has %d rows", size.width, size.height, len(lines))
		}
		for _, line := range lines {
			if lipgloss.Width(line) > size.width {
				t.Errorf("%dx%d: row wider than the window: %q", size.width, size.height, line)
			}
		}
		if !strings.Contains(m.View(), "space check") {
			t.Errorf("%dx%d: the list footer must stay visible", size.width, size.height)
		}
	}
}

func TestStatuslineSourceLineOffersTheSwitch(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.openStatusline()
	if !strings.Contains(m.statuslineView(), "s to switch") {
		t.Errorf("the source line must say how to switch the source")
	}
}

func TestStatuslineFooterDoesNotAdvertiseReordering(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.openStatusline()
	view := m.statuslineView()
	if !strings.Contains(view, "order follows the list") {
		t.Errorf("the list footer must say that the order follows the list")
	}
	if strings.Contains(view, "⇧↑⇧↓") {
		t.Errorf("the list footer must not advertise the reorder keys")
	}
}

func TestStatuslineScriptModeLeavesTheListAlone(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.openStatusline()
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.bar.cursor != 0 {
		t.Errorf("the list must not move while the script decides, cursor %d", m.bar.cursor)
	}
	if !strings.Contains(m.statuslineView(), "the script decides") {
		t.Errorf("the list footer must say that the script decides")
	}
}

func TestStatusBarKeepsTheModeLabelWhole(t *testing.T) {
	m, tb := barModelAt(t, "")
	tb.setMode(modeEdits)
	for _, width := range []int{60, 80} {
		bar := ansi.Strip(m.statusBar(tb, width))
		if !strings.Contains(bar, "Edits accepted") {
			t.Errorf("width %d: the mode label must stay whole, got %q", width, bar)
		}
	}
}

func TestStatusBarDropsTheTokenDetailBeforeTheCost(t *testing.T) {
	m, tb := barModelAt(t, "")
	bar := ansi.Strip(m.statusBar(tb, 60))
	if strings.Contains(bar, "82k/200k") {
		t.Errorf("the token detail is the first segment to leave: %q", bar)
	}
	if !strings.Contains(bar, "$1.42") {
		t.Errorf("the cost must survive the narrow bar: %q", bar)
	}
	if lipgloss.Width(bar) > 60 {
		t.Errorf("the bar must fit 60 columns: %q", bar)
	}
}

func TestStatuslineTOMLUsesTheRefreshKey(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.openStatusline()
	view := m.statuslineView()
	if !strings.Contains(view, `refresh = "300ms"`) {
		t.Errorf("the TOML pane must document the refresh key")
	}
	if strings.Contains(view, "interval") {
		t.Errorf("the TOML pane must not mention the interval key")
	}
}

func TestStatusBarKeepsTheContextLabelWhenTheGaugeFolds(t *testing.T) {
	m, tb := barModelAt(t, "")
	tb.ctxTokens = 82_000
	for width := 40; width <= 120; width++ {
		bar := ansi.Strip(m.statusBar(tb, width))
		if index := strings.Index(bar, "41%"); index >= 0 && !strings.Contains(bar[:index], "ctx") {
			t.Errorf("width %d: the percentage must keep its ctx label: %q", width, bar)
		}
	}
}

func TestQueueHeaderStaysVisibleInAShortWindow(t *testing.T) {
	m, tb := barModelAt(t, "")
	tb.busy = true
	for index := range 25 {
		tb.queue = append(tb.queue, queuedInput{text: "message " + strconv.Itoa(index)})
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	view := m.View()
	if !strings.Contains(view, "queued (25)") {
		t.Errorf("the queue header must stay visible while the transcript keeps room")
	}
	if rows := len(strings.Split(view, "\n")); rows > 30 {
		t.Errorf("the view has %d rows for a 30 row window", rows)
	}
}
