package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "github.com/charmbracelet/bubbletea"
)

func composerLines(t *testing.T, m *model) []string {
	t.Helper()
	all := strings.Split(m.View(), "\n")
	zone := len(m.composerZone())
	if len(all) < zone {
		t.Fatalf("view has %d rows, composer zone claims %d", len(all), zone)
	}
	status := len(m.statusZone())
	rows := all[len(all)-zone-status : len(all)-status]
	if m.composerBoxed() {
		rows = rows[1 : len(rows)-1]
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		r = ansi.Strip(r)
		r = strings.TrimLeft(r, "│|")
		r = strings.TrimRight(strings.TrimRight(r, " "), "│|")
		out[i] = strings.TrimSpace(r)
	}
	return out
}

func typeInto(t *testing.T, m *model, msgs ...tea.Msg) *model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(*model)
		_ = m.View()
	}
	return m
}

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

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
