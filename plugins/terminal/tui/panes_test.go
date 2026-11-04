package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
		{60, 30, []string{"alpha"}, []string{"beta", "gamma", "delta"}},
		{80, 30, []string{"alpha", "beta"}, []string{"gamma", "delta"}},
		{120, 30, []string{"alpha", "beta", "gamma", "delta"}, nil},
		{200, 30, []string{"alpha", "beta", "gamma", "delta"}, nil},
	}
	for _, tc := range cases {
		_, m := paneModel(t)
		m.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
		view := m.View()
		for _, name := range tc.visible {
			if !strings.Contains(view, name+" · ") {
				t.Fatalf("width %d: pane %q must be visible: %s", tc.width, name, view)
			}
		}
		for _, name := range tc.hidden {
			if strings.Contains(view, name+" · ") {
				t.Fatalf("width %d: pane %q must not be visible", tc.width, name)
			}
		}
		lines := strings.Split(view, "\n")
		if len(lines) > tc.height {
			t.Fatalf("width %d: %d lines for height %d", tc.width, len(lines), tc.height)
		}
		for _, line := range lines {
			if lipgloss.Width(line) > tc.width {
				t.Fatalf("width %d: line overflows: %q", tc.width, line)
			}
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
