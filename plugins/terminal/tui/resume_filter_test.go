package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestResumeFuzzyFilterAndPreview(t *testing.T) {
	f := &fakeBackend{
		fe: make(chan RoutedEvent),
		sessions: []contracts.SessionInfo{
			{Name: "alice", ChannelID: "ch-alice", Archived: true, LastTs: "2026-01-02T00:00:00Z"},
			{Name: "bob", ChannelID: "ch-bob", Archived: true, LastTs: "2026-01-01T00:00:00Z"},
		},
		scrollback: map[string][]contracts.ScrollbackLine{
			"alice": {{Role: "agent", Text: "ancien message"}, {Role: "agent", Text: "dernier message"}},
		},
	}
	m := readyModel(f)
	m.runSlash("/resume")
	pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ali")})
	view := m.View()
	if strings.Contains(view, "bob") {
		t.Fatalf("fuzzy filter must hide bob: %s", view)
	}
	if !strings.Contains(view, "dernier message") {
		t.Fatalf("preview must show the last message of the selected session: %s", view)
	}
	pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.active != "ch-alice" {
		t.Fatalf("Enter must open the filtered session, active = %q", m.active)
	}
}
