package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderTranscript turns a tab's logical entries into the styled, width-wrapped
// string the viewport displays. It runs on every sync (and thus every resize),
// so wrapping always matches the current width. A blank line precedes each
// you/agent block so turns breathe; tool/cost/scrollback lines stay tight — the
// full-width Claude flow, with no enclosing card and no role-coloured spine.
func (m *model) renderTranscript(tb *tab, width int) string {
	if width < 1 {
		width = 1
	}
	var b strings.Builder
	for i := range tb.entries {
		e := tb.entries[i]
		if i > 0 {
			b.WriteByte('\n')
			if e.role == roleYou || e.role == roleAgent {
				b.WriteByte('\n') // a fresh message gets a breathing line above it
			}
		}
		b.WriteString(renderEntry(e, width))
	}
	return b.String()
}

// renderEntry styles one entry to the given content width, in the Claude shapes:
// a user turn is a dim "> {text}" echo, agent prose is bare full-width text, a
// tool status is a warm "● call" line with dim "  ⎿ result" continuations.
func renderEntry(e entry, width int) string {
	switch e.role {
	case roleYou:
		out := wrapWith(userStyle, glyphPrompt+" "+e.text, width)
		if chips := chipRow(e.attachments); chips != "" {
			out += "\n" + chips
		}
		if e.preview != "" {
			// The kitty graphics escape sits on its own line under the chip; the
			// terminal draws the image at the cursor. Non-kitty terminals ignore it.
			out += "\n" + e.preview
		}
		return out
	case roleAgent:
		return wrapWith(textStyle, e.text, width)
	case roleStatus:
		return renderTool(e.text, width)
	case roleCost:
		return dimStyle.Render(e.text)
	case roleScrollback:
		return wrapWith(scrollbackStyle, e.text, width)
	default:
		return wrapWith(dimStyle, e.text, width)
	}
}

// renderTool renders a tool status entry Claude-style: the first line as a warm
// "● {call}" line, any following lines as dim "  ⎿ {result}" continuations. A
// single-line status is just the "● " line.
func renderTool(text string, width int) string {
	lines := strings.Split(text, "\n")
	var b strings.Builder
	b.WriteString(wrapWith(warmStyle, glyphTool+" "+lines[0], width))
	for _, ln := range lines[1:] {
		b.WriteByte('\n')
		b.WriteString(wrapWith(dimStyle, "  "+glyphResult+" "+ln, width))
	}
	return b.String()
}

// wrapWith word-wraps s to width under style, so a long line is folded
// (glyph-width aware, via lipgloss) instead of clipped by the terminal.
func wrapWith(style lipgloss.Style, s string, width int) string {
	if width < 1 {
		width = 1
	}
	return style.Width(width).Render(s)
}
