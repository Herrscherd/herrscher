package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// readyModel returns a model that has received an initial size, so the viewport
// and composer are live.
func readyModel(f *fakeBackend) *model {
	m := newModel(f)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

// TestAltEnterInsertsNewlineEnterSubmits is the core composer key policy: Alt+Enter
// adds a line (growing the composer), a plain Enter submits the whole draft.
func TestAltEnterInsertsNewlineEnterSubmits(t *testing.T) {
	f := &fakeBackend{}
	m := readyModel(f)
	m.ensureTab("a")
	m.active = "a"

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line1")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true}) // newline, not submit
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line2")})

	if got := m.input.Value(); got != "line1\nline2" {
		t.Fatalf("Alt+Enter must insert a newline: value=%q", got)
	}
	if m.composerHeight() != 2 {
		t.Fatalf("two-line draft must grow composer to 2 rows, got %d", m.composerHeight())
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // plain Enter submits
	if m.input.Value() != "" {
		t.Fatalf("Enter must submit and clear the composer: %q", m.input.Value())
	}
	if m.composerHeight() != 1 {
		t.Fatalf("composer must collapse to 1 row after submit, got %d", m.composerHeight())
	}
	tb := m.tabs["a"]
	if got := tabText(tb); !strings.Contains(got, "line1\nline2") {
		t.Fatalf("submitted multi-line draft must be echoed intact: %q", got)
	}
}

// TestComposerGrowthShrinksViewport verifies the growing composer reserves chrome
// so the transcript viewport shrinks by exactly the added rows.
func TestComposerGrowthShrinksViewport(t *testing.T) {
	m := readyModel(&fakeBackend{})
	m.ensureTab("a")
	m.active = "a"
	base := m.vp.Height
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if m.composerHeight() != 3 {
		t.Fatalf("composer height = %d, want 3", m.composerHeight())
	}
	if m.vp.Height != base-2 {
		t.Fatalf("viewport must shrink by the 2 added composer rows: base=%d now=%d", base, m.vp.Height)
	}
}

// TestComposerCapsHeight confirms the composer stops growing at maxComposerLines.
func TestComposerCapsHeight(t *testing.T) {
	m := readyModel(&fakeBackend{})
	for i := 0; i < maxComposerLines+5; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	}
	if m.composerHeight() != maxComposerLines {
		t.Fatalf("composer must cap at %d rows, got %d", maxComposerLines, m.composerHeight())
	}
}
