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
	body := width - lipgloss.Width(blockIndent)
	switch e.role {
	case roleYou:
		return renderYou(e, width)
	case roleAgent:
		// A block still arriving is incomplete markdown — an unclosed fence would
		// swallow the rest of the message — so it stays raw until endStream. It
		// also gets no closing rule: the block is not closed yet, and drawing the
		// line that says it is would be a lie redrawn on every chunk.
		if e.streaming {
			return titledRule(accentStyle, agentTitle, width) + "\n" +
				indent(wrapWith(textStyle, e.text, body))
		}
		return block(accentStyle, agentTitle, renderMarkdown(e.text, body), width)
	case roleThinking:
		return indent(wrapWith(thinkingStyle, glyphThinking+" "+e.text, body))
	case roleTool:
		return indent(renderTool(e.text, body))
	case roleNotice:
		return indent(wrapWith(dimStyle, glyphNotice+" "+e.text, body))
	case roleError:
		return block(redStyle, glyphError+" error", wrapWith(redStyle, e.text, body), width)
	case roleCost:
		return indent(dimStyle.Render(e.text))
	case roleScrollback:
		// Not a line of the conversation: the mark under the replayed history
		// saying the live session starts here. Replayed turns themselves are
		// rendered as the turns they are.
		return titledRule(scrollbackStyle, e.text, width)
	default:
		return indent(wrapWith(dimStyle, e.text, body))
	}
}

// agentTitle names the voice a framed answer belongs to. The bolt is the
// terminal's own mark, not a decoration: the tab strip and this rule are the two
// places the reader is told whose words these are.
const agentTitle = "⚡ Herrscher"

// block frames body between a titled opening rule and a plain closing one, with
// the body in the gutter. What it buys is a boundary: an answer and the tool
// lines under it are otherwise one column of text, and a reader looking for
// where the reply started has to find it by colour alone.
func block(style lipgloss.Style, title, body string, width int) string {
	return titledRule(style, title, width) + "\n" +
		indent(body) + "\n" +
		rule(style, width)
}

// renderYou renders the operator's own turn: the request framed between two
// rules, then the chips for whatever was attached to it and the inline image
// preview under them.
//
// It is framed rather than merely dim because it is the thing a reader scrolls
// back to find. Everything else on screen is the machine answering; this is the
// only line that says what was asked, and in a long transcript it is one dim row
// among hundreds.
func renderYou(e entry, width int) string {
	body := width - lipgloss.Width(blockIndent)
	out := rule(dimStyle, width) + "\n" +
		indent(wrapWith(userStyle, glyphCursor+" "+e.text, body)) + "\n" +
		rule(dimStyle, width)
	if chips := chipRow(e.attachments); chips != "" {
		out += "\n" + indent(chips)
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
