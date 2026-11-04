package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	footerSeparator = " · "
	hintChoose      = "↑↓ choisir"
)

func cutText(text string, width int) string {
	if width < 1 {
		return ""
	}
	return ansi.Truncate(text, width, ellipsis)
}

func fitSegments(segments []string, width int) string {
	joined := make([]string, len(segments))
	for index, segment := range segments {
		if index > 0 {
			segment = footerSeparator + segment
		}
		joined[index] = segment
	}
	return foldSegments(joined, width)
}

func foldSegments(segments []string, budget int) string {
	for count := len(segments); count >= 1; count-- {
		if line := strings.Join(segments[:count], ""); lipgloss.Width(line) <= budget {
			return line
		}
	}
	if len(segments) == 0 {
		return ""
	}
	return cutText(segments[0], budget)
}
