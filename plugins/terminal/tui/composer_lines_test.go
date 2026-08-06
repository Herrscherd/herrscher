package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// composerLines returns the rows View draws for the composer itself.
func composerLines(t *testing.T, m *model) []string {
	t.Helper()
	all := strings.Split(m.View(), "\n")
	if len(all) < m.composerHeight() {
		t.Fatalf("view has %d rows, composer claims %d", len(all), m.composerHeight())
	}
	out := all[len(all)-m.composerHeight():]
	for i, r := range out {
		out[i] = strings.TrimRight(r, " ")
	}
	return out
}

func typeInto(t *testing.T, m *model, msgs ...tea.Msg) *model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(*model)
		// The real loop renders after every message, and rendering is what left
		// the widget's inner viewport scrolled. A test that only pumps Update
		// never reproduces the defect.
		_ = m.View()
	}
	return m
}

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// TestComposerKeepsEveryLineOnANewline is the regression for a second line that
// ate the first: the textarea scrolls its own inner viewport when an edit pushes
// the cursor below the box, and growing the box afterwards does not undo that
// scroll. The composer showed the new line on top and a blank row beneath it,
// with the line above it gone.
func TestComposerKeepsEveryLineOnANewline(t *testing.T) {
	m := typeInto(t, newModel(nil), tea.WindowSizeMsg{Width: 100, Height: 20},
		runeKey('A'), tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, runeKey('B'))

	if got := m.input.Value(); got != "A\nB" {
		t.Fatalf("composer holds %q", got)
	}
	want := []string{"A", "B"}
	got := composerLines(t, m)
	if len(got) != len(want) {
		t.Fatalf("composer drew %d rows %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("composer row %d is %q, want %q (all rows: %q)", i, got[i], want[i], got)
		}
	}
}

// TestComposerKeepsEveryLineWhenTheDraftWraps is the same defect reached by
// typing rather than by a chord: the character that spills past the width moves
// the cursor onto a row the box does not have yet.
func TestComposerKeepsEveryLineWhenTheDraftWraps(t *testing.T) {
	m := typeInto(t, newModel(nil), tea.WindowSizeMsg{Width: 20, Height: 20})
	for _, r := range "aaaaaaaaaa bbbbbbbbbb cc" {
		m = typeInto(t, m, runeKey(r))
	}
	rows := composerLines(t, m)
	if len(rows) < 2 {
		t.Fatalf("a wrapped draft drew %d rows: %q", len(rows), rows)
	}
	if !strings.HasPrefix(rows[0], "aaaaaaaaaa") {
		t.Fatalf("the first row of a wrapped draft is %q, want the text it started with (all rows: %q)", rows[0], rows)
	}
}

// TestComposerStopsGrowingAtItsCap: past maxComposerLines the widget scrolls,
// which is the behaviour openComposerBox must not suppress — the cap exists so a
// long draft never eats the transcript.
func TestComposerStopsGrowingAtItsCap(t *testing.T) {
	m := typeInto(t, newModel(nil), tea.WindowSizeMsg{Width: 100, Height: 40})
	for i := 0; i < maxComposerLines+4; i++ {
		m = typeInto(t, m, runeKey('x'), tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	}
	if h := m.composerHeight(); h != maxComposerLines {
		t.Fatalf("composer grew to %d rows, want the %d-row cap", h, maxComposerLines)
	}
	if rows := composerLines(t, m); len(rows) != maxComposerLines {
		t.Fatalf("composer drew %d rows, want %d", len(rows), maxComposerLines)
	}
}
