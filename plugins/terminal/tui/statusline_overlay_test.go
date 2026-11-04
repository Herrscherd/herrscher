package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestStatuslineCommandOpensTheOverlayBelowTheComposer(t *testing.T) {
	m, _ := barModelAt(t, "")
	if _, handled := m.runLocalSlash("/statusline", []string{"statusline"}); !handled {
		t.Fatal("/statusline must be a local command")
	}
	if !strings.Contains(strings.Join(m.buildFrame().below, "\n"), "/statusline") {
		t.Errorf("the overlay must show under the frame when open")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if strings.Contains(strings.Join(m.buildFrame().below, "\n"), "/statusline") {
		t.Errorf("esc must close the overlay")
	}
}

func TestUntickedFieldLeavesTheBar(t *testing.T) {
	m, tb := barModelAt(t, "")
	if !strings.Contains(m.statusBar(tb, 200), "$1.42") {
		t.Fatal("the cost must show before it is unticked")
	}
	m.openStatusline()
	ids := m.bar.fieldIDs()
	for i, id := range ids {
		if id == "cost" {
			for range i {
				m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
			}
			break
		}
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if strings.Contains(m.statusBar(tb, 200), "$1.42") {
		t.Errorf("an unticked cost must leave the bar: %q", m.statusBar(tb, 200))
	}
}

func TestStatuslineNotesStartOnTheSameColumn(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.openStatusline()
	lines := strings.Split(m.statuslineView(), "\n")
	var columns []int
	for _, note := range []string{"claude, codex", "herrscher:feat", "(worktree)", "go test -race"} {
		for _, line := range lines {
			if index := strings.Index(line, note); index >= 0 {
				columns = append(columns, lipgloss.Width(line[:index]))
				break
			}
		}
	}
	if len(columns) != 4 {
		t.Fatalf("found %d notes in the overlay, want 4", len(columns))
	}
	for _, column := range columns {
		if column != columns[0] {
			t.Errorf("notes start at columns %v, want one column", columns)
		}
	}
}
