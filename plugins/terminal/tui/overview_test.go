package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const boardOverviewJSON = `[
 {"name":"alpha","vendor":"claude","gateways":["terminal"],"state":"idle","cost":0.5,"last_ts":"2026-01-01T10:00:00Z","last_text":"fini"},
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
	working, waiting, done := listRow(view, "gamma"), listRow(view, "beta"), listRow(view, "alpha")
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

func TestBoardFitsEveryWidthAndHeight(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		_, m := boardModel(t)
		m.width, m.height = width, 20
		updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlP})
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) > 20 {
			t.Fatalf("at %d columns the board is %d lines high, window is 20", width, len(lines))
		}
		for _, line := range lines {
			if w := lipgloss.Width(line); w > width {
				t.Fatalf("at %d columns a board line is %d wide: %q", width, w, line)
			}
		}
		if !strings.Contains(view, "alpha") || !strings.Contains(view, "gamma") {
			t.Fatalf("at %d columns the board must list the sessions: %s", width, view)
		}
	}
}

func TestBoardShowsCostAndGatewayWhenThereIsRoom(t *testing.T) {
	_, m := boardModel(t)
	m.width, m.height = 200, 20
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	view := m.View()
	if !strings.Contains(view, "$0.50") || !strings.Contains(view, "terminal") {
		t.Fatalf("a wide board must show the cost and the gateway: %s", view)
	}
}

func TestBoardListsEachSessionWithItsStateGlyph(t *testing.T) {
	_, m := boardModel(t)
	m.width, m.height = 120, 30
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	view := m.View()
	glyphs := m.st.glyphs
	for _, want := range []string{glyphs.attention, glyphs.open} {
		if !strings.Contains(view, want) {
			t.Fatalf("board must show the glyph %q for the session states: %s", want, view)
		}
	}
}

func TestBoardFooterNamesTheKeys(t *testing.T) {
	_, m := boardModel(t)
	m.width, m.height = 120, 30
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	view := m.View()
	for _, want := range []string{"ouvrir", "échap vider"} {
		if !strings.Contains(view, want) {
			t.Fatalf("board footer must name the key %q: %s", want, view)
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
	review := listRow(view, "À relire")
	if review < 0 {
		t.Fatalf("review group missing from board: %s", view)
	}
	if gamma := listRow(view, "gamma"); gamma < review {
		t.Fatalf("gamma must sit under À relire, view: %s", view)
	}
}

func listRow(view, name string) int {
	for y, line := range strings.Split(view, "\n") {
		list, _, _ := strings.Cut(line, "│")
		if strings.Contains(list, name) {
			return y
		}
	}
	return -1
}
