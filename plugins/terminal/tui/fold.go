package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func foldableEntry(item entry) bool {
	switch item.role {
	case roleThinking:
		return item.text != ""
	case roleTool:
		return strings.Contains(item.text, "\n")
	}
	return false
}

func (m *model) openOrFold() {
	if _, ok := m.selectedLink(); ok {
		m.openSelectedLink()
		return
	}
	tab := m.tabs[m.active]
	if tab == nil {
		return
	}
	for index := len(tab.entries) - 1; index >= 0; index-- {
		if foldableEntry(tab.entries[index]) {
			m.toggleEntryFold(tab, tab.entries[index].id)
			return
		}
	}
	m.flash = m.tr.t("tui.render.nothing_to_fold", nil)
}

func (m *model) toggleEntryFold(tab *tab, entryID uint64) {
	if tab.flips == nil {
		tab.flips = make(map[uint64]bool)
	}
	tab.flips[entryID] = !tab.flips[entryID]
	m.tsCache.valid = false
	m.syncViewport()
}

func (m *model) clickTranscript(msg tea.MouseMsg) bool {
	tab := m.tabs[m.active]
	if tab == nil || m.foldTurnsOn {
		return false
	}
	row := msg.Y - m.frameTop() + m.vp.YOffset
	if row < 0 || row >= len(m.tsLayout.owners) {
		return false
	}
	owner := m.tsLayout.owners[row]
	for index := range tab.entries {
		if tab.entries[index].id == owner && owner != 0 && foldableEntry(tab.entries[index]) {
			m.toggleEntryFold(tab, owner)
			return true
		}
	}
	return false
}
