package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const brand = "──◂ HERRSCHER ▸──"

func (m *model) railRow() string {
	left := accentStyle.Render(brand)
	strip := m.tabStrip()
	right := m.vendorMark()
	if strip == "" {
		return truncate(joinRow(m.innerWidth(), left, right), m.innerWidth())
	}
	gap := m.innerWidth() - lipgloss.Width(left) - lipgloss.Width(strip) - lipgloss.Width(right)
	if gap >= 4 {
		return left + strings.Repeat(" ", gap-2) + strip + "  " + right
	}
	if lipgloss.Width(strip) <= m.innerWidth() {
		return strip
	}
	return truncate(m.activeMark(), m.innerWidth())
}

func (m *model) activeMark() string {
	tb := m.tabs[m.active]
	if tb == nil {
		return ""
	}
	return accentStyle.Render(glyphCursor + " " + tb.label)
}

func (m *model) tabStrip() string {
	var out []string
	for _, ch := range m.order {
		tb := m.tabs[ch]
		if tb == nil {
			continue
		}
		label := tb.label
		if tb.unread {
			label = glyphUnread + label
		}
		if ch == m.active {
			out = append(out, accentStyle.Render(glyphCursor+" "+label))
			continue
		}
		out = append(out, dimStyle.Render(label))
	}
	return strings.Join(out, "  ")
}

func (m *model) vendorMark() string {
	s, ok := m.activeInfo()
	if !ok || s.Vendor == "" {
		return ""
	}
	return dimStyle.Render(glyphBolt + " " + s.Vendor)
}

func joinRow(width int, left, right string) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}
