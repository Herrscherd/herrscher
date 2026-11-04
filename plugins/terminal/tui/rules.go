package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const titleGap = " "

const blockIndent = "   "

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = blockIndent + ln
	}
	return strings.Join(lines, "\n")
}

const gaugeWidth = 16

func gauge(ratio float64, style, empty lipgloss.Style, glyphs glyphSet) string {
	return empty.Render("[") + barGauge(ratio, gaugeWidth, style, empty, glyphs) + empty.Render("]")
}

func barGauge(ratio float64, cells int, style, empty lipgloss.Style, glyphs glyphSet) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(cells))
	if filled == 0 && ratio > 0 {
		filled = 1
	}
	return style.Render(strings.Repeat(glyphs.gaugeFull, filled)) +
		empty.Render(strings.Repeat(glyphs.gaugeEmpty, cells-filled))
}
