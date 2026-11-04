package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type transcriptLayout struct {
	text   string
	owners []uint64
	turns  []int
}

func (m *model) renderTranscript(tab *tab, width int) string {
	return m.layoutTranscript(tab, width).text
}

func (m *model) layoutTranscript(tab *tab, width int) transcriptLayout {
	if width < 1 {
		width = 1
	}
	now := time.Now()
	treeOwner := lastAgentToolIndex(tab.entries)
	var lines []string
	var owners []uint64
	var turns []int
	for index := range tab.entries {
		item := m.enrichEntry(tab, index, now)
		if index > 0 && (item.role == roleYou || item.role == roleAgent) {
			lines = append(lines, "")
			owners = append(owners, 0)
		}
		if item.role == roleYou || item.role == roleAgent {
			turns = append(turns, len(lines))
		}
		if m.foldCode && item.role == roleAgent && !item.streaming {
			item.text = foldCodeBlocks(item.text)
		}
		for _, ln := range strings.Split(renderEntry(item, width, m.displayMode()), "\n") {
			lines = append(lines, ln)
			owners = append(owners, item.id)
		}
		if index == treeOwner && len(tab.trail) > 0 {
			rows := subagentRows(tab.trail, now, width-lipgloss.Width(blockIndent), m.displayMode())
			for _, ln := range strings.Split(indent(strings.Join(rows, "\n")), "\n") {
				lines = append(lines, ln)
				owners = append(owners, item.id)
			}
		}
	}
	m.links = findLinks(lines)
	m.clampLink()
	return transcriptLayout{
		text:   strings.Join(applyLinks(lines, m.links, m.caps, m.linkIdx, m.st), "\n"),
		owners: owners,
		turns:  turns,
	}
}

func (m *model) clampLink() {
	if m.linkIdx >= len(m.links) {
		m.linkIdx = len(m.links) - 1
	}
	if m.linkIdx < -1 {
		m.linkIdx = -1
	}
}

func lastAgentToolIndex(entries []entry) int {
	for index := len(entries) - 1; index >= 0; index-- {
		if entries[index].role != roleTool {
			continue
		}
		if family, _ := toolVerb(entries[index].text); family == familyAgent {
			return index
		}
	}
	return -1
}

func (m *model) enrichEntry(tab *tab, index int, now time.Time) entry {
	item := tab.entries[index]
	last := index == len(tab.entries)-1
	end := item.ended
	if end.IsZero() && index+1 < len(tab.entries) {
		end = tab.entries[index+1].at
	}
	if last && tab.busy {
		end = now
	}
	if !end.IsZero() {
		item.span = end.Sub(item.at)
	}
	item.live = last && tab.busy && item.role == roleTool
	item.failed = item.role == roleTool && index+1 < len(tab.entries) && tab.entries[index+1].role == roleError
	if item.role == roleThinking {
		item.tokens = max(0, item.tokens-item.base)
	}
	item.foldable = foldableEntry(item)
	item.open = tab.flips[item.id] != defaultOpen(item)
	return item
}

func defaultOpen(item entry) bool {
	return item.role == roleTool && isDiffBody(strings.Split(item.text, "\n")[1:])
}

type view struct {
	caps Capabilities
	st   uiStyles
	md   *markdownRenderer

	expand bool
	spin   string
}

func (m *model) displayMode() view {
	return view{caps: m.caps, st: m.st, md: m.md, expand: m.expandTools, spin: m.spinFrame()}
}

func renderEntry(item entry, width int, view view) string {
	body := width - lipgloss.Width(blockIndent)
	switch item.role {
	case roleYou:
		return renderYou(item, width, view)
	case roleAgent:
		return renderAgent(item, width, view)
	case roleThinking:
		return indent(renderThinking(item, body, view))
	case roleTool:
		return indent(renderTool(item, body, view))
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

func renderAgent(item entry, width int, view view) string {
	lead := glyphTool + " "
	body := width - lipgloss.Width(blockIndent) - lipgloss.Width(lead)
	var text string
	if item.streaming {
		text = wrapWith(view.st.text, item.text, body)
	} else {
		text = renderMarkdown(item.text, body, view)
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		prefix := strings.Repeat(" ", lipgloss.Width(lead))
		if i == 0 {
			prefix = view.st.text.Render(lead)
		}
		lines[i] = prefix + lines[i]
	}
	out := indent(strings.Join(lines, "\n"))
	if item.preview != "" {
		out += "\n" + item.preview
	}
	return out
}

func block(style lipgloss.Style, title, body string, width int) string {
	return titledRule(style, title, width) + "\n" +
		indent(body) + "\n" +
		rule(style, width)
}

func renderYou(item entry, width int, v view) string {
	lead := glyphUserBar + " "
	body := width - lipgloss.Width(blockIndent) - lipgloss.Width(lead)
	bar := v.st.accent.Render(lead)
	var lines []string
	for _, ln := range strings.Split(wrapWith(v.st.text, item.text, body), "\n") {
		lines = append(lines, bar+ln)
	}
	out := indent(strings.Join(lines, "\n"))
	if chips := chipRow(item.attachments, v.st); chips != "" {
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
