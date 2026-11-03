package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const brand = "──◂ HERRSCHER ▸──"

const (
	railMinGap    = 4
	railSeparator = "  "
)

func (m *model) railRow() string {
	left := m.st.accent.Render(brand)
	strip := m.tabStrip()
	right := m.vendorMark()
	if strip == "" {
		return truncate(joinRow(m.innerWidth(), left, right), m.innerWidth())
	}
	gap := m.innerWidth() - lipgloss.Width(left) - lipgloss.Width(strip) - lipgloss.Width(right)
	if gap >= railMinGap {
		return left + strings.Repeat(" ", gap-len(railSeparator)) + strip + railSeparator + right
	}
	if lipgloss.Width(strip) <= m.innerWidth() {
		return strip
	}
	return truncate(m.activeMark(), m.innerWidth())
}

func (m *model) activeMark() string {
	tab := m.tabs[m.active]
	if tab == nil {
		return ""
	}
	return m.st.accent.Render(glyphCursor + " " + tab.label)
}

func (m *model) tabStrip() string {
	var out []string
	for _, channel := range m.order {
		tab := m.tabs[channel]
		if tab == nil {
			continue
		}
		label := tab.label
		if tab.unread {
			label = glyphUnread + label
		}
		if channel == m.active {
			out = append(out, m.st.accent.Render(glyphCursor+" "+label))
			continue
		}
		out = append(out, m.st.dim.Render(label))
	}
	return strings.Join(out, "  ")
}

func (m *model) vendorMark() string {
	info, ok := m.activeInfo()
	if !ok || info.Vendor == "" {
		return ""
	}
	return m.st.dim.Render(glyphBolt + " " + info.Vendor)
}

func joinRow(width int, left, right string) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}
