package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// A rule is the horizontal line that opens or closes a block. Colour alone was
// carrying the whole distinction between a request, an answer and an error, and
// colour is the one channel a terminal is free to take away — a 16-colour
// profile, a light background, a colourblind reader. A ruled block keeps its
// shape when its colour does not survive.

// ruleChar is the line a rule is drawn with.
const ruleChar = "─"

// titleGap is the blank on each side of a rule's title, so the text does not
// touch the line.
const titleGap = " "

// rule draws a full-width line under style.
func rule(style lipgloss.Style, width int) string {
	if width < 1 {
		return ""
	}
	return style.Render(strings.Repeat(ruleChar, width))
}

// titledRule draws `─ <title> ─────`: a short lead-in, the title, then the line
// running out to width. It is what opens a block that has something to announce
// — whose voice this is, or that what follows went wrong.
//
// A width too small for the title falls back to the plain rule rather than
// clipping the title to something unreadable: a truncated word says less than a
// line that is honestly just a line.
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

// blockIndent is the gutter a framed block's body sits in, so the body reads as
// belonging to the rule above it rather than as the next thing along.
const blockIndent = "   "

// indent shifts every line of s into the block gutter. It is applied after
// styling and wrapping, so the gutter is uniform even where a line was folded.
func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = blockIndent + ln
	}
	return strings.Join(lines, "\n")
}

// gaugeWidth is how many cells the context gauge occupies. It is fixed: a gauge
// that resized with the window would make the same occupancy look different from
// one moment to the next.
const gaugeWidth = 10

// The gauge's two cells: filled for what is spent, empty for what is left.
const (
	gaugeFull  = "█"
	gaugeEmpty = "░"
)

// gauge renders occupancy as a bar. The point is that a bar is read without
// being read: the eye takes in how full it is before the number beside it
// resolves into a quantity. Its colour is the palier's, so an alarming context
// is alarming at a glance and not only on inspection.
func gauge(ratio float64, style lipgloss.Style) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	// Anything spent at all shows at least one cell: an empty bar must mean
	// nothing sent, not merely little.
	filled := int(ratio * gaugeWidth)
	if filled == 0 && ratio > 0 {
		filled = 1
	}
	// Each segment is styled whole and concatenated: a style nested inside
	// another ends with its own reset, which would strip the colour from
	// everything after it — the closing bracket included.
	return dimStyle.Render("[") +
		style.Render(strings.Repeat(gaugeFull, filled)) +
		dimStyle.Render(strings.Repeat(gaugeEmpty, gaugeWidth-filled)+"]")
}
