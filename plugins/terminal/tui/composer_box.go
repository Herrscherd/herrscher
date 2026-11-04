package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	composerChrome     = 4
	composerBorderCols = 2
)

const composerBoxMinWidth = 60

func (m *model) composerBoxed() bool {
	return m.innerWidth() >= composerBoxMinWidth
}

func (m *model) composerTextWidth() int {
	if !m.composerBoxed() {
		return m.innerWidth()
	}
	return m.innerWidth() - composerChrome
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
	g := m.st.boxes(m.caps.Boxes)
	topLeft, topRight, bottomLeft, bottomRight, horizontal, vertical := g.tl, g.tr, g.bl, g.br, g.h, g.v
	width := m.innerWidth()
	edge := strings.Repeat(horizontal, width-composerBorderCols)
	side := m.st.dim.Render(vertical)
	zone := []string{m.st.dim.Render(topLeft + edge + topRight)}
	for _, line := range body {
		line = truncate(line, width-composerChrome)
		pad := width - composerChrome - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		zone = append(zone, side+" "+line+strings.Repeat(" ", pad)+" "+side)
	}
	zone = append(zone, m.st.dim.Render(bottomLeft+edge+bottomRight))
	return zone
}
