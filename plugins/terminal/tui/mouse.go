package tui

import tea "github.com/charmbracelet/bubbletea"

func (m *model) toggleMouse() tea.Cmd {
	m.mouseFree = !m.mouseFree
	if m.mouseFree {
		m.flash = joinNonEmpty("souris libérée", "glisser pour sélectionner comme d’habitude", m.pairHint(actScrollPageUp, actScrollPageDown, "défiler", chatKeyStack()...), m.labelledHint(actToggleMouse, "la reprend", chatKeyStack()...))
		return tea.DisableMouse
	}
	m.flash = "souris reprise · la molette fait de nouveau défiler la conversation"
	return tea.EnableMouseCellMotion
}

func (m *model) mouseStatus() string {
	if !m.mouseFree {
		return ""
	}
	return m.st.dim.Render("souris → terminal")
}
