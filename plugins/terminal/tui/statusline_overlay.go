package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const statuslineFooter = "↑↓ choisir · espace cocher · ⇧↑⇧↓ ordre · s source · échap fermer"

func (m *model) openStatusline() tea.Cmd {
	m.bar.open = true
	m.bar.cursor = 0
	m.relayout()
	return m.armStatusline()
}

func (m *model) closeStatusline() {
	m.bar.open = false
	if err := saveStatuslineChoice(m.bar.file, &m.bar); err != nil {
		m.flash = "terminal.toml non écrit : " + err.Error()
	}
	m.relayout()
}

func (m *model) handleStatuslineKey(msg tea.KeyMsg) tea.Cmd {
	if m.overlayNav(msg, m.moveStatuslineCursor) {
		return nil
	}
	switch msg.String() {
	case keyEsc:
		m.closeStatusline()
	case keyCtrlC:
		return m.overlayCtrlC()
	case "shift+up":
		m.moveStatuslineField(-1)
	case "shift+down":
		m.moveStatuslineField(1)
	case " ":
		m.toggleStatuslineField()
	case "s":
		return m.switchStatuslineSource()
	}
	return nil
}

func (m *model) moveStatuslineCursor(delta int) {
	last := len(m.bar.fieldIDs()) - 1
	m.bar.cursor = max(0, min(m.bar.cursor+delta, last))
}

func (m *model) moveStatuslineField(delta int) {
	if m.bar.moveField(m.bar.cursor, delta) {
		m.moveStatuslineCursor(delta)
	}
}

func (m *model) toggleStatuslineField() {
	ids := m.bar.fieldIDs()
	if m.bar.cursor < len(ids) {
		m.bar.toggle(ids[m.bar.cursor])
	}
}

func (m *model) switchStatuslineSource() tea.Cmd {
	m.bar.script.line = ""
	m.bar.script.failed = false
	if m.bar.scripted() {
		m.bar.config.Source = barSourceBuiltin
		return nil
	}
	m.bar.config.Source = barSourceScript
	return m.armStatusline()
}

func (m *model) statuslineView() string {
	ids := m.bar.fieldIDs()
	rows := make([]overlayRow, 0, len(ids))
	for _, fieldID := range ids {
		spec := barFieldSpecOf(fieldID)
		mark := m.st.glyphs.unticked
		if m.bar.isShown(fieldID) {
			mark = m.st.glyphs.ticked
		}
		rows = append(rows, overlayRow{mark: mark + " ", label: spec.labelWith(m.st.glyphs), detail: " " + spec.note})
	}
	start, end := paletteWindow(len(rows), m.bar.cursor, m.overlayRows())
	list := floatingList(rows[start:end], m.bar.cursor-start, statuslineFooter, m.innerWidth(), m.st)
	lines := []string{
		statusIndent + m.st.accent.Render("Barre d’état") + m.st.dim.Render(" · /statusline"),
		m.statuslineSourceLine(),
		"",
		list,
		"",
		statusIndent + m.st.dim.Render("aperçu en direct, ligne suivante : la barre telle qu’elle sera dans le terminal"),
		statusIndent + m.statusLeftPreview(),
	}
	return strings.Join(lines, "\n")
}

func (m *model) statuslineSourceLine() string {
	builtin, script := m.st.dim, m.st.dim
	if m.bar.scripted() {
		script = m.st.text
	} else {
		builtin = m.st.text
	}
	return statusIndent + m.st.dim.Render("source  ") +
		builtin.Render("intégrée") + m.st.dim.Render("  ") +
		script.Render("script")
}

func (m *model) statusLeftPreview() string {
	target := m.tabs[m.active]
	if target == nil {
		return ""
	}
	return m.statusBar(target, m.innerWidth()-lipgloss.Width(statusIndent))
}
