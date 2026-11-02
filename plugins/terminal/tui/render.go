package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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
				b.WriteByte('\n')
			}
		}
		if m.foldCode && e.role == roleAgent && !e.streaming {
			e.text = foldCodeBlocks(e.text)
		}
		b.WriteString(renderEntry(e, width, m.displayMode()))
	}
	lines := strings.Split(b.String(), "\n")
	m.links = findLinks(lines)
	m.clampLink()
	return strings.Join(applyLinks(lines, m.links, m.caps, m.linkIdx, m.st), "\n")
}

func (m *model) clampLink() {
	if m.linkIdx >= len(m.links) {
		m.linkIdx = len(m.links) - 1
	}
	if m.linkIdx < -1 {
		m.linkIdx = -1
	}
}

type view struct {
	caps Capabilities
	st   uiStyles
	md   *markdownRenderer

	expand bool
}

func (m *model) displayMode() view {
	return view{caps: m.caps, st: m.st, md: m.md, expand: m.expandTools}
}

func renderEntry(e entry, width int, v view) string {
	body := width - lipgloss.Width(blockIndent)
	switch e.role {
	case roleYou:
		return renderYou(e, width, v.st)
	case roleAgent:
		if e.streaming {
			return titledRule(v.st.accent, agentTitle, width) + "\n" +
				indent(wrapWith(v.st.text, e.text, body))
		}
		out := openBlock(v.st.accent, agentTitle, renderMarkdown(e.text, body, v), width)
		if e.preview != "" {
			out += "\n" + e.preview
		}
		return out
	case roleThinking:
		return indent(wrapWith(v.st.thinking, thinkingLine(e.text), body))
	case roleTool:
		return indent(renderTool(e.text, body, v))
	case roleNotice:
		return indent(wrapWith(v.st.dim, glyphNotice+" "+e.text, body))
	case roleError:
		return block(v.st.red, glyphError+" error", wrapWith(v.st.red, e.text, body), width)
	case roleCost:
		return indent(v.st.dim.Render(e.text))
	case roleScrollback:
		return titledRule(v.st.scrollback, e.text, width)
	default:
		return indent(wrapWith(v.st.dim, e.text, body))
	}
}

const agentTitle = "⚡ Herrscher"

func block(style lipgloss.Style, title, body string, width int) string {
	return titledRule(style, title, width) + "\n" +
		indent(body) + "\n" +
		rule(style, width)
}

func openBlock(style lipgloss.Style, title, body string, width int) string {
	return titledRule(style, title, width) + "\n" + indent(body)
}

func renderYou(e entry, width int, st uiStyles) string {
	body := width - lipgloss.Width(blockIndent)
	out := indent(wrapWith(st.user, glyphCursor+" "+e.text, body))
	if chips := chipRow(e.attachments, st); chips != "" {
		out += "\n" + indent(chips)
	}
	if e.preview != "" {
		out += "\n" + e.preview
	}
	return out
}

func wrapWith(style lipgloss.Style, s string, width int) string {
	if width < 1 {
		width = 1
	}
	return style.Width(width).Render(s)
}
