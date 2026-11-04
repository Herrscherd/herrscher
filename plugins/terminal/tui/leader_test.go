package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLeaderTableHoldsEditorAndEnter(t *testing.T) {
	for _, key := range []string{"ctrl+e", "enter"} {
		if _, ok := leaderBindings()[key]; !ok {
			t.Fatalf("leader table misses %q", key)
		}
	}
}

func TestLeaderUnknownKeyIsNotHandled(t *testing.T) {
	m, _ := queueFixture(t)
	m.leaderArmed = time.Now()
	if _, ok := m.handleLeader(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")}); ok {
		t.Fatal("an unknown leader key must fall through")
	}
}
