package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
