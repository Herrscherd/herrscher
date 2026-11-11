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
	width = max(width, 1)
	now := time.Now()
	treeOwner := lastAgentToolIndex(tab.entries)
	var built transcriptBuilder
	for index := range tab.entries {
		item := m.enrichEntry(tab, index, now)
		isTurn := item.role == roleYou || item.role == roleAgent
		if index > 0 && isTurn {
			built.addBlank()
		}
		if isTurn {
			built.turns = append(built.turns, len(built.lines))
		}
		if m.foldCode && item.role == roleAgent && !item.streaming {
			item.text = foldCodeBlocks(m.tr, item.text, m.st.glyphs, m.hintIn(actToggleCodeFold, chatKeyStack()...))
		}
		built.add(strings.Split(renderEntry(item, width, m.displayMode()), "\n"), item.id)
		if index == treeOwner && len(tab.trail) > 0 {
			rows := subagentRows(tab.trail, now, width-lipgloss.Width(blockIndent), m.displayMode())
			built.add(strings.Split(indent(strings.Join(rows, "\n")), "\n"), item.id)
		}
	}
	m.links = findLinks(built.lines)
	m.clampLink()
	return transcriptLayout{
		text:   strings.Join(applyLinks(built.lines, m.links, m.caps, m.linkIdx, m.st), "\n"),
		owners: built.owners,
		turns:  built.turns,
	}
}

type transcriptBuilder struct {
	lines  []string
	owners []uint64
	turns  []int
}

func (b *transcriptBuilder) add(lines []string, owner uint64) {
	for _, line := range lines {
		b.lines = append(b.lines, line)
		b.owners = append(b.owners, owner)
	}
}

func (b *transcriptBuilder) addBlank() {
	b.add([]string{""}, 0)
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
		item.span = max(0, end.Sub(item.at))
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
	tr   translator
	caps Capabilities
	st   uiStyles
	md   *markdownRenderer
	keys *keyTable

	expand bool
	spin   string
}

func (m *model) displayMode() view {
	return view{tr: m.tr, caps: m.caps, st: m.st, md: m.md, keys: m.bindings, expand: m.expandTools, spin: m.spinFrame()}
}

func (v view) glyph(action keyAction) string {
	table := v.keys
	if table == nil {
		table = defaultKeyTable(v.tr)
	}
	return table.hintFor(v.tr, action, chatKeyStack()...)
}

func (v view) hint(action keyAction, label string) string {
	return joinWith(" ", v.glyph(action), label)
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
		return indent(renderNotice(item, body, view))
	case roleError:
		return block(view.st, view.st.red, view.st.glyphs.failure+" "+view.tr.t("tui.render.error_label", nil), wrapWith(view.st.red, item.text, body), width)
	case roleCost:
		return indent(view.st.dim.Render(item.text))
	case roleScrollback:
		return view.st.titledRule(view.st.scrollback, item.text, width)
	default:
		return indent(wrapWith(view.st.dim, item.text, body))
	}
}

func renderNotice(item entry, width int, view view) string {
	text := view.st.glyphs.notice + " " + item.text
	if !item.markdown {
		return wrapWith(view.st.dim, text, width)
	}
	styled := styleInlineMarkdown(text, view.st.dim, view.st.dim.Bold(true), view.st.accent)
	return lipgloss.NewStyle().Width(max(width, 1)).Render(styled)
}

func renderAgent(item entry, width int, view view) string {
	lead := view.st.glyphs.tool + " "
	body := width - lipgloss.Width(blockIndent) - lipgloss.Width(lead)
	var text string
	if item.streaming {
		text = wrapWith(view.st.text, item.text, body)
	} else {
		text = renderMarkdown(item.text, body, view)
	}
	lines := strings.Split(text, "\n")
	for index := range lines {
		prefix := strings.Repeat(" ", lipgloss.Width(lead))
		if index == 0 {
			prefix = view.st.text.Render(lead)
		}
		lines[index] = prefix + lines[index]
	}

	out := indent(strings.Join(lines, "\n"))
	if item.preview != "" {
		out += "\n" + item.preview
	}
	return out
}

func block(styles uiStyles, style lipgloss.Style, title, body string, width int) string {
	return styles.titledRule(style, title, width) + "\n" +
		indent(body) + "\n" +
		styles.rule(style, width)
}

func renderYou(item entry, width int, display view) string {
	lead := display.st.glyphs.userBar + " "
	body := width - lipgloss.Width(blockIndent) - lipgloss.Width(lead)
	bar := display.st.accent.Render(lead)
	var lines []string
	for _, ln := range strings.Split(wrapWith(display.st.text, item.text, body), "\n") {
		lines = append(lines, bar+ln)
	}
	out := indent(strings.Join(lines, "\n"))
	if chips := chipRow(item.attachments, display.st); chips != "" {
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
