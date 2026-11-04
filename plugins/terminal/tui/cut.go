package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	cutMark         = "…"
	footerSeparator = " · "
	hintChoose      = "↑↓ choisir"
)

func cutText(text string, width int) string {
	if width < 1 {
		return ""
	}
	return ansi.Truncate(text, width, cutMark)
}

func fitSegments(segments []string, width int) string {
	kept := segments
	for len(kept) > 1 && lipgloss.Width(strings.Join(kept, footerSeparator)) > width {
		kept = kept[:len(kept)-1]
	}
	return cutText(strings.Join(kept, footerSeparator), width)
}
