package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *model) statusZone() []string {
	left := m.footer()
	if m.flash != "" {
		left = dimStyle.Render("· " + m.flash)
	}
	left = "  " + left
	if m.innerWidth() < narrowStatus {
		return []string{truncate(left, m.innerWidth())}
	}
	right := m.hintText() + "  "
	gap := m.innerWidth() - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return []string{truncate(left, m.innerWidth())}
	}
	return []string{left + strings.Repeat(" ", gap) + right}
}
