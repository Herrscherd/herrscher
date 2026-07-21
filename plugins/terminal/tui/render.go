package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderTranscript turns a tab's logical entries into the styled, width-wrapped
// string the viewport displays. It runs on every sync (and thus every resize),
// so wrapping always matches the current width — the fix for the old
// pre-rendered, never-re-wrapped lines. A blank line precedes each you/agent
// block so turns breathe; status/cost/scrollback lines stay tight.
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

// renderEntry styles one entry to the given content width. you/agent turns become
// gutter blocks; status/cost/scrollback stay as dim single wrapped lines.
func renderEntry(e entry, width int) string {
	switch e.role {
	case roleYou:
		out := block(youGutter, humanStyle.Render(glyphYou+" you"), e.text, humanBodyStyle, e.attachments, width)
		if e.preview != "" {
			// The kitty graphics escape sits on its own line under the chip; the
			// terminal draws the image at the cursor. Non-kitty terminals ignore it.
			out += "\n" + e.preview
		}
		return out
	case roleAgent:
		return block(agentGutter, replyStyle.Render(glyphAgent+" agent"), e.text, agentBodyStyle, nil, width)
	case roleCost:
		return costStyle.Render(e.text)
	case roleScrollback:
		return wrapWith(scrollbackStyle, e.text, width)
	default: // roleStatus
		return wrapWith(statusStyle, "· "+e.text, width)
	}
}

// block renders a message as a role-coloured spine: a header line, then the body
// word-wrapped to width-2 (the "▎ " gutter) with every wrapped line prefixed by
// the spine, and finally any attachment chips on their own spined line. An empty
// body with no attachments yields just the header.
func block(gutter lipgloss.Style, header, body string, bodyStyle lipgloss.Style, atts []Attachment, width int) string {
	bar := gutter.Render(glyphGutter)
	var b strings.Builder
	b.WriteString(bar + " " + header)
	if strings.TrimSpace(body) != "" {
		wrapWidth := width - 2
		if wrapWidth < 1 {
			wrapWidth = 1
		}
		wrapped := bodyStyle.Width(wrapWidth).Render(body)
		for _, ln := range strings.Split(wrapped, "\n") {
			b.WriteByte('\n')
			b.WriteString(bar + " " + ln)
		}
	}
	if chips := chipRow(atts); chips != "" {
		b.WriteByte('\n')
		b.WriteString(bar + " " + chips)
	}
	return b.String()
}

// wrapWith word-wraps s to width under style, so a long status/scrollback line is
// folded (glyph-width aware, via lipgloss) instead of clipped by the terminal.
func wrapWith(style lipgloss.Style, s string, width int) string {
	if width < 1 {
		width = 1
	}
	return style.Width(width).Render(s)
}
