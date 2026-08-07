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
	// Links are found on the finished, wrapped lines rather than on the entry
	// text: what the operator selects is what is on screen, and a URL folded
	// across a wrap is not one link but two halves of a line.
	lines := strings.Split(b.String(), "\n")
	m.links = findLinks(lines)
	m.clampLink()
	return strings.Join(applyLinks(lines, m.links, m.caps, m.linkIdx), "\n")
}

// clampLink keeps the selection inside the current link set. A turn that arrived
// and re-flowed the transcript must not leave the gesture aimed at a link that
// is no longer there; -1 means nothing is selected, which is the resting state.
func (m *model) clampLink() {
	if m.linkIdx >= len(m.links) {
		m.linkIdx = len(m.links) - 1
	}
	if m.linkIdx < -1 {
		m.linkIdx = -1
	}
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
		out := openBlock(accentStyle, agentTitle, renderMarkdown(e.text, body), width)
		if e.preview != "" {
			// An image the answer linked to, fetched and drawn under it. The URL
			// stays in the prose above: the picture is an addition, not a
			// replacement, and a reader still needs to be able to copy the link.
			out += "\n" + e.preview
		}
		return out
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
// the body in the gutter. Reserved for what should stop the eye — an error is
// rare enough that a closed frame costs nothing and reads as an interruption of
// the conversation, which is what it is.
func block(style lipgloss.Style, title, body string, width int) string {
	return titledRule(style, title, width) + "\n" +
		indent(body) + "\n" +
		rule(style, width)
}

// openBlock is block without the closing rule: a titled rule saying whose words
// follow, then the words. The closing rule said nothing the blank line and the
// next speaker's rule did not already say, and it cost a row on every single
// turn — a transcript of short exchanges came out roughly half horizontal lines.
func openBlock(style lipgloss.Style, title, body string, width int) string {
	return titledRule(style, title, width) + "\n" + indent(body)
}

// renderYou renders the operator's own turn: the request in the gutter behind
// the cursor glyph, then the chips for whatever was attached to it and the
// inline image preview under them.
//
// It used to be framed between two rules, on the reasoning that it is the line a
// reader scrolls back to find. But a one-word message then cost four rows for one
// character, and a frame drawn around every turn stops marking anything. The
// glyph and the colour are what actually distinguish it, and they survive being
// scrolled past.
func renderYou(e entry, width int) string {
	body := width - lipgloss.Width(blockIndent)
	out := indent(wrapWith(userStyle, glyphCursor+" "+e.text, body))
	if chips := chipRow(e.attachments); chips != "" {
		out += "\n" + indent(chips)
	}
	if e.preview != "" {
		// The image sits on its own line under the chip, in whatever encoding the
		// probe said this terminal draws — down to half-blocks, which always work.
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
