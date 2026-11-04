package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	minStatusGap = 2
	statusIndent = "  "
)

func (m *model) statusZone() []string {
	width := m.innerWidth()
	right := ""
	if width >= narrowStatus {
		right = m.hintText() + "  "
	}
	budget := width - lipgloss.Width(statusIndent)
	if right != "" {
		budget -= lipgloss.Width(right) + minStatusGap
	}
	left := statusIndent + m.statusLeft(budget)
	if right == "" {
		return []string{truncate(left, width)}
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	return []string{left + strings.Repeat(" ", gap) + right}
}

func (m *model) statusLeft(budget int) string {
	if m.flash != "" {
		return truncate(m.st.dim.Render("· "+m.flash), budget)
	}
	return m.footer(budget)
}
