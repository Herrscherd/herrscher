package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderTranscript turns a tab's logical entries into the styled, width-wrapped
// string the viewport displays. It runs on every sync (and thus every resize),
// so wrapping always matches the current width. A blank line precedes each
// you/agent block so turns breathe; tool/cost/scrollback lines stay tight.
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

// renderEntry dispatches one entry to the renderer for its role. Each role owns
// its own shape — that is the whole point of having roles: a tool call, a piece
// of reasoning and an error are three different things, and a reader should be
// able to tell them apart without reading them.
func renderEntry(e entry, width int) string {
	switch e.role {
	case roleYou:
		return renderYou(e, width)
	case roleAgent:
		// A block still arriving is incomplete markdown — an unclosed fence would
		// swallow the rest of the message — so it stays raw until endStream.
		if e.streaming {
			return wrapWith(textStyle, e.text, width)
		}
		return renderMarkdown(e.text, width)
	case roleThinking:
		return wrapWith(thinkingStyle, glyphThinking+" "+e.text, width)
	case roleTool:
		return renderTool(e.text, width)
	case roleNotice:
		return wrapWith(dimStyle, glyphNotice+" "+e.text, width)
	case roleError:
		return wrapWith(redStyle, glyphError+" "+e.text, width)
	case roleCost:
		return dimStyle.Render(e.text)
	case roleScrollback:
		return wrapWith(scrollbackStyle, e.text, width)
	default:
		return wrapWith(dimStyle, e.text, width)
	}
}

// renderYou renders the operator's own turn: the dim "> " echo, then the chips
// for whatever was attached to it and the inline image preview under them.
func renderYou(e entry, width int) string {
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
}

// toolFamilies maps a backend tool name to the family it is rendered as. The
// vocabulary is closed on purpose: the bus carries a tool line as one opaque
// string (the hub flattens name and detail together in emitBackendEvent), so
// the name can only be recovered by reading the first word. Guessing wider than
// a known list would mistype ordinary prose that happens to start with a verb.
var toolFamilies = map[string]string{
	"read":         familyRead,
	"glob":         familyRead,
	"grep":         familySearch,
	"websearch":    familySearch,
	"webfetch":     familyWeb,
	"edit":         familyWrite,
	"write":        familyWrite,
	"notebookedit": familyWrite,
	"bash":         familyRun,
	"task":         familyAgent,
	"agent":        familyAgent,
	"skill":        familyAgent,
}

// The tool families, each with its own glyph so a scan down the gutter reads as
// what the agent has been doing rather than as one undifferentiated column.
const (
	familyRead   = "read"
	familyWrite  = "write"
	familyRun    = "run"
	familySearch = "search"
	familyWeb    = "web"
	familyAgent  = "agent"
)

// toolVerb splits a tool status line into the family it belongs to and the rest
// of the line. An unrecognised first word yields an empty family and the line
// untouched — an unknown tool must stay readable, never disappear.
func toolVerb(text string) (family, rest string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", ""
	}
	head, tail, _ := strings.Cut(trimmed, " ")
	fam, ok := toolFamilies[strings.ToLower(head)]
	if !ok {
		return "", trimmed
	}
	return fam, strings.TrimSpace(tail)
}

// familyWidth pads the family column so targets line up down the gutter. It is
// the longest family name plus a separating space.
const familyWidth = 7

// renderTool renders a tool status entry: a typed glyph and the tool's target on
// the first line, any following lines as dim continuations. A line whose first
// word is not a known tool keeps the generic glyph and its full text.
//
// The line is styled as a whole rather than by segment: a nested style ends with
// its own reset, which would strip the colour from everything after it.
func renderTool(text string, width int) string {
	lines := strings.Split(text, "\n")
	family, rest := toolVerb(lines[0])
	head := familyGlyph(family) + " "
	if family != "" {
		head += family + strings.Repeat(" ", max(1, familyWidth-len(family)))
	}
	head += rest
	var b strings.Builder
	b.WriteString(wrapWith(toolStyle, head, width))
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
