package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const historyFooter = "⌃R historique · ⏎ reprendre · échap fermer"

func (m *model) openHistSearch() {
	m.histOpen = true
	m.histQuery = ""
	m.histSel = 0
}

func (m *model) closeHistSearch() {
	m.histOpen = false
	m.histQuery = ""
	m.histSel = 0
}

func (m *model) histMatches() []string {
	newestFirst := make([]string, 0, len(m.history))
	for i := len(m.history) - 1; i >= 0; i-- {
		newestFirst = append(newestFirst, m.history[i])
	}
	return rankFuzzy(m.histQuery, newestFirst)
}

func (m *model) moveHistSel(delta int) {
	count := len(m.histMatches())
	m.histSel += delta
	if m.histSel >= count {
		m.histSel = count - 1
	}
	if m.histSel < 0 {
		m.histSel = 0
	}
}

func (m *model) handleHistSearchKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m.overlayCtrlC()
	case tea.KeyEsc:
		m.closeHistSearch()
	case tea.KeyEnter:
		if matches := m.histMatches(); len(matches) > 0 {
			m.input.SetValue(matches[m.histSel])
			m.input.CursorEnd()
			m.resizeComposer()
		}
		m.closeHistSearch()
	case tea.KeyUp, tea.KeyCtrlP:
		m.moveHistSel(-1)
	case tea.KeyDown, tea.KeyCtrlN, tea.KeyCtrlR:
		m.moveHistSel(1)
	case tea.KeyBackspace:
		if runes := []rune(m.histQuery); len(runes) > 0 {
			m.histQuery = string(runes[:len(runes)-1])
			m.histSel = 0
		}
	case tea.KeySpace:
		m.histQuery += " "
		m.histSel = 0
	case tea.KeyRunes:
		if !msg.Alt {
			m.histQuery += string(msg.Runes)
			m.histSel = 0
		}
	}
	return nil
}

func (m *model) histView() string {
	matches := m.histMatches()
	if len(matches) == 0 {
		return m.st.dim.Render("  (aucune correspondance)")
	}
	visible := m.overlayRows()
	start, _ := paletteWindow(len(matches), m.histSel, visible)
	end := start + visible
	if end > len(matches) {
		end = len(matches)
	}
	rows := make([]overlayRow, 0, end-start)
	for _, text := range matches[start:end] {
		line := oneLine(text)
		row := overlayRow{label: line}
		if hit, ok := fuzzyMatch(m.histQuery, line); ok {
			row.hot = hit.positions
		}
		rows = append(rows, row)
	}
	footer := historyFooter
	if q := strings.TrimSpace(m.histQuery); q != "" {
		footer = "« " + q + " » · " + footer
	}
	return floatingList(rows, m.histSel-start, footer, m.overlayWidth(), m.st)
}
