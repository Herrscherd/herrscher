package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const boardOverviewJSON = `[
 {"name":"alpha","vendor":"claude","gateways":["terminal"],"state":"done","cost":0.5,"last_ts":"2026-01-01T10:00:00Z","last_text":"fini"},
 {"name":"beta","vendor":"claude","gateways":["discord"],"state":"waiting","cost":1.25,"last_ts":"2026-01-01T10:05:00Z","last_text":"ok ?"},
 {"name":"gamma","vendor":"codex","gateways":["terminal"],"state":"working","cost":0.1,"last_ts":"2026-01-01T10:09:00Z","last_text":"en route"}
]`

func boardModel(t *testing.T) (*fakeBackend, *model) {
	t.Helper()
	f := &fakeBackend{
		fe: make(chan RoutedEvent),
		sessions: []contracts.SessionInfo{
			{Name: "alpha", ChannelID: "ch-alpha"},
			{Name: "beta", ChannelID: "ch-beta"},
			{Name: "gamma", ChannelID: "ch-gamma"},
		},
		overview: boardOverviewJSON,
	}
	return f, readyModel(f)
}

func updateKey(m *model, msg tea.KeyMsg) {
	_, cmd := m.Update(msg)
	settle(m, cmd)
}

func TestBoardGroupsSessionsByState(t *testing.T) {
	_, m := boardModel(t)
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	view := m.View()
	for _, label := range []string{"Au travail", "Attend une réponse", "Fini"} {
		if !strings.Contains(view, label) {
			t.Fatalf("board must show group %q: %s", label, view)
		}
	}
	working, waiting, done := strings.Index(view, "gamma"), strings.Index(view, "beta"), strings.Index(view, "alpha")
	if working < 0 || working >= waiting || waiting >= done {
		t.Fatalf("groups must read working, waiting, done: gamma=%d beta=%d alpha=%d", working, waiting, done)
	}
}

func TestBoardEnterOpensSelectedSession(t *testing.T) {
	_, m := boardModel(t)
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	updateKey(m, tea.KeyMsg{Type: tea.KeyDown})
	updateKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.surface != surfaceChat {
		t.Fatal("Enter must leave the board")
	}
	if m.active != "ch-beta" {
		t.Fatalf("Enter must switch to the selected session, active = %q", m.active)
	}
}

func TestBoardFilterNarrowsRowsByLetters(t *testing.T) {
	_, m := boardModel(t)
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	updateKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ga")})
	view := m.View()
	if strings.Contains(view, "alpha") || strings.Contains(view, "beta") {
		t.Fatalf("filter must hide non matching sessions: %s", view)
	}
	updateKey(m, tea.KeyMsg{Type: tea.KeyBackspace})
	updateKey(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if !strings.Contains(m.View(), "alpha") {
		t.Fatal("backspace must clear the filter")
	}
}

func TestBoardHidesCostThenGatewayWhenNarrow(t *testing.T) {
	_, m := boardModel(t)
	m.openBoard()
	wide := m.boardView(200, 20)
	if !strings.Contains(wide, "$0.50") || !strings.Contains(wide, "terminal") {
		t.Fatalf("wide board must show cost and gateway: %s", wide)
	}
	medium := m.boardView(60, 20)
	if strings.Contains(medium, "$0.50") || !strings.Contains(medium, "terminal") {
		t.Fatalf("at 60 columns cost goes first, gateway stays: %s", medium)
	}
	narrow := m.boardView(40, 20)
	if strings.Contains(narrow, "terminal") {
		t.Fatalf("at 40 columns the gateway goes too: %s", narrow)
	}
	for _, line := range strings.Split(narrow, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("board line overflows 40 columns: %q", line)
		}
	}
}

func TestParseSessionOverviewsReadsHostJSON(t *testing.T) {
	overviews, err := ParseSessionOverviews(boardOverviewJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(overviews) != 3 || overviews[1].State != "waiting" || overviews[1].Cost != 1.25 {
		t.Fatalf("overviews = %+v", overviews)
	}
	if _, err := ParseSessionOverviews("pas du json"); err == nil {
		t.Fatal("garbled overview must be an error")
	}
}

func TestFinishedSessionNotReadIsListedUnderReview(t *testing.T) {
	f, m := boardModel(t)
	f.overview = `[{"name":"gamma","state":"review","cost":0.1}]`
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	view := m.View()
	review := strings.Index(view, "À relire")
	if review < 0 {
		t.Fatalf("review group missing from board: %s", view)
	}
	if gamma := strings.Index(view, "gamma"); gamma < review {
		t.Fatalf("gamma must sit under À relire, view: %s", view)
	}
}
