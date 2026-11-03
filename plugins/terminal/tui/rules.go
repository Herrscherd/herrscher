package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const ruleChar = "─"

const titleGap = " "

func rule(style lipgloss.Style, width int) string {
	if width < 1 {
		return ""
	}
	return style.Render(strings.Repeat(ruleChar, width))
}

func titledRule(style lipgloss.Style, title string, width int) string {
	if width < 1 {
		return ""
	}
	lead := ruleChar + titleGap
	tail := titleGap
	spent := lipgloss.Width(lead) + lipgloss.Width(title) + lipgloss.Width(tail)
	if spent >= width {
		return rule(style, width)
	}
	return style.Render(lead) + style.Bold(true).Render(title) +
		style.Render(tail+strings.Repeat(ruleChar, width-spent))
}

const blockIndent = "   "

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = blockIndent + ln
	}
	return strings.Join(lines, "\n")
}

const gaugeWidth = 10

const (
	gaugeFull  = "█"
	gaugeEmpty = "░"
)

func gauge(ratio float64, style, empty lipgloss.Style) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * gaugeWidth)
	if filled == 0 && ratio > 0 {
		filled = 1
	}
	return empty.Render("[") +
		style.Render(strings.Repeat(gaugeFull, filled)) +
		empty.Render(strings.Repeat(gaugeEmpty, gaugeWidth-filled)+"]")
}
