package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const composerBoxMinWidth = 60

func boxRunes(supported bool) (topLeft, topRight, bottomLeft, bottomRight, horizontal, vertical string) {
	if !supported {
		return "+", "+", "+", "+", "-", "|"
	}
	return "╭", "╮", "╰", "╯", "─", "│"
}

func (m *model) composerBoxed() bool {
	return m.innerWidth() >= composerBoxMinWidth
}

func (m *model) composerTextWidth() int {
	if !m.composerBoxed() {
		return m.innerWidth()
	}
	return m.innerWidth() - 4
}

func (m *model) composerZone() []string {
	body := strings.Split(m.inputRow(), "\n")
	if !m.composerBoxed() {
		out := make([]string, len(body))
		for i, line := range body {
			out[i] = truncate(line, m.innerWidth())
		}
		return out
	}
	tl, tr, bl, br, h, v := boxRunes(m.caps.Boxes)
	width := m.innerWidth()
	edge := strings.Repeat(h, width-2)
	side := dimStyle.Render(v)
	zone := []string{dimStyle.Render(tl + edge + tr)}
	for _, line := range body {
		line = truncate(line, width-4)
		pad := width - 4 - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		zone = append(zone, side+" "+line+strings.Repeat(" ", pad)+" "+side)
	}
	zone = append(zone, dimStyle.Render(bl+edge+br))
	return zone
}
