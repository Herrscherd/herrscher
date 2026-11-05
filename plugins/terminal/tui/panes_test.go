package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func paneModel(t *testing.T) (*fakeBackend, *model) {
	t.Helper()
	f := &fakeBackend{
		fe: make(chan RoutedEvent),
		sessions: []contracts.SessionInfo{
			{Name: "alpha", ChannelID: "ch-alpha", Vendor: "claude"},
			{Name: "beta", ChannelID: "ch-beta", Vendor: "claude"},
			{Name: "gamma", ChannelID: "ch-gamma", Vendor: "codex"},
			{Name: "delta", ChannelID: "ch-delta", Vendor: "claude"},
		},
		overview: `[
		 {"name":"alpha","state":"working","cost":0.2},
		 {"name":"beta","state":"waiting","cost":0.4},
		 {"name":"gamma","state":"done","cost":0.1},
		 {"name":"delta","state":"done","cost":0.3}]`,
	}
	m := readyModel(f)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	return f, m
}

func TestPaneGridFitsEachWidth(t *testing.T) {
	cases := []struct {
		width, height int
		visible       []string
		hidden        []string
	}{
		{60, 30, []string{"alpha", "beta"}, []string{"gamma", "delta"}},
		{80, 30, []string{"alpha", "beta"}, []string{"gamma", "delta"}},
		{120, 30, []string{"alpha", "beta", "gamma", "delta"}, nil},
		{200, 30, []string{"alpha", "beta", "gamma", "delta"}, nil},
	}
	for _, tc := range cases {
		_, m := paneModel(t)
		m.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
		assertPaneGrid(t, m.View(), tc.width, tc.height, tc.visible, tc.hidden)
	}
}

func assertPaneGrid(t *testing.T, view string, width, height int, visible, hidden []string) {
	t.Helper()
	for _, name := range visible {
		if !strings.Contains(view, name+" · ") {
			t.Fatalf("width %d: pane %q must be visible: %s", width, name, view)
		}
	}
	for _, name := range hidden {
		if strings.Contains(view, name+" · ") {
			t.Fatalf("width %d: pane %q must not be visible", width, name)
		}
	}
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		t.Fatalf("width %d: %d lines for height %d", width, len(lines), height)
	}
	for _, line := range lines {
		if lipgloss.Width(line) > width {
			t.Fatalf("width %d: line overflows: %q", width, line)
		}
	}
}

func TestPaneShowsVendorGatewayAndCost(t *testing.T) {
	_, m := paneModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	view := m.View()
	if !strings.Contains(view, "claude") || !strings.Contains(view, "$0.20") {
		t.Fatalf("pane header must carry vendor and cost: %s", view)
	}
}

func TestPaneLetterWithoutApprovalGoesToComposer(t *testing.T) {
	f, m := paneModel(t)
	updateKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if len(f.submitted) != 0 {
		t.Fatalf("o without pending approval must not submit: %+v", f.submitted)
	}
	if m.input.Value() != "o" {
		t.Fatalf("o must be typed in the composer, got %q", m.input.Value())
	}
}

func TestPaneEnterSendsToFocusedPane(t *testing.T) {
	f, m := paneModel(t)
	updateKey(m, tea.KeyMsg{Type: tea.KeyTab})
	m.input.SetValue("bonjour")
	updateKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(f.submitted) != 1 || f.submitted[0].channel != "ch-beta" || f.submitted[0].text != "bonjour" {
		t.Fatalf("Enter must send to the focused pane: %+v", f.submitted)
	}
}

func paneCellPosition(t *testing.T, m *model, header string) (int, int) {
	t.Helper()
	for y, line := range strings.Split(m.View(), "\n") {
		if index := strings.Index(line, header); index >= 0 {
			return lipgloss.Width(line[:index]), y
		}
	}
	t.Fatalf("no pane header %q on screen", header)
	return 0, 0
}

func TestPaneClickFocusesThePaneUnderThePointer(t *testing.T) {
	_, m := paneModel(t)
	x, y := paneCellPosition(t, m, "beta · ")
	m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if got := m.panes.channels[m.panes.focus]; got != "ch-beta" {
		t.Fatalf("a left click on beta must focus it, focused channel = %q", got)
	}
}

func TestPaneReleaseDoesNotMoveFocus(t *testing.T) {
	_, m := paneModel(t)
	before := m.panes.focus
	x, y := paneCellPosition(t, m, "gamma · ")
	m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if m.panes.focus != before {
		t.Fatalf("a release must not move focus, focus = %d, was %d", m.panes.focus, before)
	}
}

func TestPaneScreenNamesItsHeaderAndKeys(t *testing.T) {
	_, m := paneModel(t)
	view := m.View()
	for _, want := range []string{"Vue en volets", "clic pour viser", "⇥ volet suivant · clic viser"} {
		if !strings.Contains(view, want) {
			t.Fatalf("panes screen must show %q: %s", want, view)
		}
	}
}

func TestNarrowPanesReachTheRowsBelow(t *testing.T) {
	_, m := paneModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	updateKey(m, tea.KeyMsg{Type: tea.KeyTab})
	updateKey(m, tea.KeyMsg{Type: tea.KeyTab})
	view := m.View()
	if !strings.Contains(view, "gamma · ") {
		t.Fatalf("a narrow grid must bring the hidden rows into view with Tab: %s", view)
	}
}

func TestNarrowPanesSayHowManyAreHidden(t *testing.T) {
	_, m := paneModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if view := m.View(); !strings.Contains(view, "+2 volets · ⇥ pour les voir") {
		t.Fatalf("a narrow grid must count the panes it hides: %s", view)
	}
}

func TestNarrowPanesKeepTheFocusedPaneVisible(t *testing.T) {
	_, m := paneModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	for _, channel := range []string{"ch-alpha", "ch-beta", "ch-gamma", "ch-delta", "ch-alpha"} {
		focusPaneChannel(t, m, channel)
		name := m.infoForChannel(channel).Name
		if view := m.View(); !strings.Contains(view, name+" · ") {
			t.Fatalf("the focused pane %s must be shown at 60x20: %s", name, view)
		}
	}
}

func TestPaneToolRowCutsTheTargetBeforeItsState(t *testing.T) {
	_, m := paneModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.tabs["ch-alpha"].entries = append(m.tabs["ch-alpha"].entries, entry{
		role: roleTool,
		text: "Bash go test ./plugins/terminal/tui/ ./plugins/terminal/...",
		span: 1200 * time.Millisecond,
	})
	for _, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		if !strings.Contains(line, "Bash") {
			continue
		}
		trimmed := strings.TrimRight(line, " │")
		if !strings.HasSuffix(trimmed, "✓") || !strings.Contains(trimmed, "…") {
			t.Fatalf("a cut pane tool row must keep its state after an ellipsis: %q", line)
		}
		return
	}
	t.Fatal("the pane must show the tool row at 80x30")
}

func TestPaneComposerShowsThePanePlaceholder(t *testing.T) {
	_, m := paneModel(t)
	m.buildFrame()
	if m.input.Placeholder != placeholderPanes {
		t.Fatalf("the panes composer must say where the message goes, got %q", m.input.Placeholder)
	}
}

func TestPaneFooterWithoutApprovalOnlyNamesNavigation(t *testing.T) {
	_, m := paneModel(t)
	view := m.View()
	if !strings.Contains(view, "⇥ volet suivant · clic viser") {
		t.Fatalf("the pane footer must name navigation: %s", view)
	}
	if strings.Contains(view, "approbation de") {
		t.Fatalf("the pane footer must not offer approvals without a pending one: %s", view)
	}
}

func focusPaneChannel(t *testing.T, m *model, channel string) {
	t.Helper()
	for range len(m.panes.channels) {
		if m.focusedChannel() == channel {
			return
		}
		updateKey(m, tea.KeyMsg{Type: tea.KeyTab})
	}
	t.Fatalf("pane %s must be reachable with Tab, panes = %v", channel, m.panes.channels)
}

func TestPaneFooterNamesTheApprovalOfTheFocusedPane(t *testing.T) {
	_, m := paneModel(t)
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "ch-beta"}, Event: approvalMenuEvent(t, "make deploy", allApprovalRoles()...)})
	focusPaneChannel(t, m, "ch-beta")
	view := m.View()
	if !strings.Contains(view, "o s r approbation de beta") {
		t.Fatalf("the pane footer must name the approval of the focused pane: %s", view)
	}
	if strings.Contains(view, "docs-site") {
		t.Fatalf("the pane footer must take its name from the session, not from a fixture: %s", view)
	}
}

func TestPaneApprovalKeyAnswersTheFocusedPaneOnly(t *testing.T) {
	f, m := paneModel(t)
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "ch-beta"}, Event: approvalMenuEvent(t, "make deploy", allApprovalRoles()...)})
	focusPaneChannel(t, m, "ch-beta")
	pressRunes(m, "o")
	if len(f.picks) != 1 || f.picks[0] != (pickedChoice{channel: "ch-beta", value: testApprovalValue(approvalAllow)}) {
		t.Fatalf("o must answer the approval of the focused pane, picks = %+v", f.picks)
	}
}

func TestPaneApprovalOutsideTheFocusIsNeitherShownNorAnswered(t *testing.T) {
	f, m := paneModel(t)
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "ch-gamma"}, Event: approvalMenuEvent(t, "make deploy", allApprovalRoles()...)})
	focusPaneChannel(t, m, "ch-alpha")
	if view := m.View(); strings.Contains(view, "approbation de") {
		t.Fatalf("a pending approval on an unfocused pane must not be offered: %s", view)
	}
	pressRunes(m, "o")
	if len(f.picks) != 0 {
		t.Fatalf("o must not answer an approval outside the focused pane, picks = %+v", f.picks)
	}
}

func TestFocusingAPaneMakesItTheComposerTarget(t *testing.T) {
	_, m := paneModel(t)
	focusPaneChannel(t, m, "ch-delta")
	if m.active != "ch-delta" {
		t.Fatalf("the active tab must follow the focused pane, active = %q", m.active)
	}
}

func TestClosingTheFocusedPaneLeavesNoGhostForTab(t *testing.T) {
	_, m := paneModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	focusPaneChannel(t, m, "ch-beta")
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "ch-beta"}, Event: contracts.Event{T: "closed"}})
	if focused := m.focusedChannel(); focused != m.active {
		t.Fatalf("the focused pane %q must be the active session %q", focused, m.active)
	}
	updateKey(m, tea.KeyMsg{Type: tea.KeyTab})
	if focused := m.focusedChannel(); focused == "ch-beta" || focused != m.active {
		t.Fatalf("Tab must land on a live pane and make it active, got focus %q, active %q", focused, m.active)
	}
}
