package tui

import tea "github.com/charmbracelet/bubbletea"

func (m *model) toggleMouse() tea.Cmd {
	m.mouseFree = !m.mouseFree
	if m.mouseFree {
		m.flash = joinNonEmpty(m.tr.t("tui.mouse.freed", nil), m.tr.t("tui.mouse.drag_select", nil), m.pairHint(actScrollPageUp, actScrollPageDown, m.tr.t("tui.mouse.scroll", nil), chatKeyStack()...), m.labelledHint(actToggleMouse, m.tr.t("tui.mouse.resume_hint", nil), chatKeyStack()...))
		return tea.DisableMouse
	}
	m.flash = m.tr.t("tui.mouse.resumed", nil)
	return tea.EnableMouseCellMotion
}

func (m *model) mouseStatus() string {
	if !m.mouseFree {
		return ""
	}
	return m.st.dim.Render(m.tr.t("tui.mouse.status", nil))
}
