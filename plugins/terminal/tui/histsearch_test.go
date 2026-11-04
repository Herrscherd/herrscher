package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCtrlRFiltersHistoryAndEnterRecalls(t *testing.T) {
	m, _ := queueFixture(t)
	m.history = []string{"alpha", "beta gamma", "delta"}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if !m.histOpen {
		t.Fatal("ctrl+r must open the history search")
	}
	for _, r := range "gam" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.histMatches(); len(got) != 1 || got[0] != "beta gamma" {
		t.Fatalf("fuzzy history matches = %v", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.histOpen || m.input.Value() != "beta gamma" {
		t.Fatalf("enter must recall the selected entry: open=%v input=%q", m.histOpen, m.input.Value())
	}
}

func TestHistorySearchNewestFirstWithoutQuery(t *testing.T) {
	m, _ := queueFixture(t)
	m.history = []string{"ancien", "récent"}
	m.openHistSearch()
	if got := m.histMatches(); len(got) != 2 || got[0] != "récent" {
		t.Fatalf("empty query must list newest first: %v", got)
	}
}

func TestEscClosesHistorySearchWithoutTouchingInput(t *testing.T) {
	m, _ := queueFixture(t)
	m.history = []string{"alpha"}
	typeLine(m, "en cours")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.histOpen || m.input.Value() != "en cours" {
		t.Fatalf("esc must close without changing the input: open=%v input=%q", m.histOpen, m.input.Value())
	}
}

func TestCtrlFOpensSearchNotHistory(t *testing.T) {
	m, _ := queueFixture(t)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	if m.histOpen {
		t.Fatal("ctrl+f must not open the history search")
	}
	if !m.searchOpen {
		t.Fatal("ctrl+f must open the transcript search")
	}
}
