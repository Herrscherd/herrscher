package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestComposerRemeasuresOnResize(t *testing.T) {
	m := readyModel(&fakeBackend{})
	m.ensureTab("a")
	m.active = "a"
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("a", 60))})
	if m.composerHeight() != 1 {
		t.Fatalf("60 chars at width 100 should be 1 row, got %d", m.composerHeight())
	}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 30})
	if m.composerHeight() < 2 {
		t.Fatalf("after narrowing to 40, 60 chars must wrap to >=2 rows, got %d", m.composerHeight())
	}
}
