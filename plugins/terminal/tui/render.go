package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *model) renderTranscript(tab *tab, width int) string {
	if width < 1 {
		width = 1
	}
	var builder strings.Builder
	for index := range tab.entries {
		item := tab.entries[index]
		if index > 0 {
			builder.WriteByte('\n')
			if item.role == roleYou || item.role == roleAgent {
				builder.WriteByte('\n')
			}
		}
		if m.foldCode && item.role == roleAgent && !item.streaming {
			item.text = foldCodeBlocks(item.text)
		}
		builder.WriteString(renderEntry(item, width, m.displayMode()))
	}
	lines := strings.Split(builder.String(), "\n")
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

func renderEntry(item entry, width int, view view) string {
	body := width - lipgloss.Width(blockIndent)
	switch item.role {
	case roleYou:
		return renderYou(item, width, view.st)
	case roleAgent:
		if item.streaming {
			return titledRule(view.st.accent, agentTitle, width) + "\n" +
				indent(wrapWith(view.st.text, item.text, body))
		}
		out := openBlock(view.st.accent, agentTitle, renderMarkdown(item.text, body, view), width)
		if item.preview != "" {
			out += "\n" + item.preview
		}
		return out
	case roleThinking:
		return indent(wrapWith(view.st.thinking, thinkingLine(item.text), body))
	case roleTool:
		return indent(renderTool(item.text, body, view))
	case roleNotice:
		return indent(wrapWith(view.st.dim, glyphNotice+" "+item.text, body))
	case roleError:
		return block(view.st.red, glyphError+" error", wrapWith(view.st.red, item.text, body), width)
	case roleCost:
		return indent(view.st.dim.Render(item.text))
	case roleScrollback:
		return titledRule(view.st.scrollback, item.text, width)
	default:
		return indent(wrapWith(view.st.dim, item.text, body))
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

func renderYou(item entry, width int, styles uiStyles) string {
	body := width - lipgloss.Width(blockIndent)
	out := indent(wrapWith(styles.user, glyphCursor+" "+item.text, body))
	if chips := chipRow(item.attachments, styles); chips != "" {
		out += "\n" + indent(chips)
	}
	if item.preview != "" {
		out += "\n" + item.preview
	}
	return out
}

func wrapWith(style lipgloss.Style, text string, width int) string {
	if width < 1 {
		width = 1
	}
	return style.Width(width).Render(text)
}
