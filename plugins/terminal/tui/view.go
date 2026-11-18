package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type overlayCache struct {
	in  string
	arg string
	out string
}

func (c *overlayCache) get(input, arg string, apply func(string) string) string {
	if c.out != "" && c.in == input && c.arg == arg {
		return c.out
	}
	out := apply(input)
	*c = overlayCache{in: input, arg: arg, out: out}
	return out
}

type transcriptCache struct {
	ch      string
	width   int
	entries int
	lastLen int
	lastStr bool
	out     string
	valid   bool
}

func (m *model) syncViewport() {
	if !m.ready {
		return
	}
	if m.tabs[m.active] == nil {
		m.vp.SetContent(m.emptyState(m.vp.Width))
		return
	}
	atBottom := m.vp.AtBottom()
	m.vp.SetContent(m.thinkingContent())
	if atBottom {
		m.vp.GotoBottom()
	}
}

func (m *model) anyBusy() bool {
	for _, tb := range m.tabs {
		if tb.busy {
			return true
		}
	}
	return false
}

func (m *model) spinFrame() string { return m.st.spinFrame(m.spin) }

func (m *model) thinkingContent() string {
	return m.contentWith(m.searchQuery)
}

func (m *model) baseContent() string { return m.contentWith("") }

func (m *model) contentWith(query string) string {
	tab := m.tabs[m.active]
	if tab == nil || (len(tab.entries) == 0 && !tab.busy && !tab.awaitingHistory()) {
		return m.emptyState(m.vp.Width)
	}
	content := m.cachedTranscript(tab)
	if tab.awaitingHistory() {
		loading := m.st.dim.Render(m.tr.t("tui.session.history_loading", nil))
		if content == "" {
			content = loading
		} else {
			content += "\n" + loading
		}
	}
	if m.foldTurnsOn {
		content = m.foldCache.get(content, "", func(s string) string {
			return strings.Join(foldTurns(m.tr, strings.Split(s, "\n"), m.tsLayout.turns, turnFoldKeep, m.st), "\n")
		})
	}
	if m.searchOpen && strings.TrimSpace(query) != "" {
		content = m.markCache.get(content, query, func(s string) string {
			return strings.Join(highlightMatches(strings.Split(s, "\n"), query), "\n")
		})
	}
	if tab.busy {
		line := m.busyRow(tab)
		if content != "" {
			content += "\n" + line
		} else {
			content = line
		}
	}
	return content
}

func (m *model) cachedTranscript(tab *tab) string {
	width := m.vp.Width
	lastLen, lastStr := 0, false
	if count := len(tab.entries); count > 0 {
		lastLen = len(tab.entries[count-1].text)
		lastStr = tab.entries[count-1].streaming
	}
	cache := &m.tsCache
	if cache.valid && cache.ch == tab.channel && cache.width == width &&
		cache.entries == len(tab.entries) && cache.lastLen == lastLen && cache.lastStr == lastStr {
		return cache.out
	}
	layout := m.layoutTranscript(tab, width)
	m.tsLayout = layout
	out := layout.text
	*cache = transcriptCache{
		ch: tab.channel, width: width, entries: len(tab.entries),
		lastLen: lastLen, lastStr: lastStr, out: out, valid: true,
	}
	return out
}

func (m *model) inputRow() string {
	lines := strings.Split(m.input.View(), "\n")
	if h := m.composerHeight(); h < len(lines) {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

func (m *model) hintText() string {
	if m.paletteOpen() {
		return m.st.dim.Render(joinNonEmpty(
			m.labelledHint(actSelectPrevious, m.tr.t("tui.palette.navigate", nil), ctxAutocomplete),
			m.labelledHint(actSelectPageDown, m.tr.t("tui.palette.page", nil), ctxAutocomplete),
			m.labelledHint(actAutoAccept, m.tr.t("tui.palette.complete", nil), ctxAutocomplete),
			m.labelledHint(actAutoDismiss, m.tr.t("tui.common.close", nil), ctxAutocomplete),
		))
	}
	return m.st.dim.Render(m.labelledHint(actHelpToggle, m.tr.t("tui.common.help", nil), chatKeyStack()...))
}

func (m *model) footer(width int) string {
	if s := m.linkStatus(); s != "" {
		return truncate(s, width)
	}
	tab := m.tabs[m.active]
	if tab == nil {
		return ""
	}
	if tab.disconnected {
		return m.st.dim.Render(m.tr.t("tui.status.disconnected", nil))
	}
	return m.statusBar(tab, width)
}

func (m *model) spinnerHint(tab *tab) string {
	segs := []string{m.labelledHint(actCancel, m.tr.t("tui.spinner.interrupt", nil), chatKeyStack()...)}
	if !tab.startedAt.IsZero() {
		segs = append(segs, fmt.Sprintf("%ds", int(time.Since(tab.startedAt).Seconds())))
	}
	if tab.tokens > 0 {
		segs = append(segs, "↑ "+formatTokens(tab.tokens))
	}
	if tab.lastCost > 0 {
		segs = append(segs, formatCost(tab.lastCost))
	}
	return m.spinFrame() + " " + m.tr.t("tui.common.working", nil) + " (" + strings.Join(segs, " · ") + ")"
}

const minDisplayedCost = 0.01

func formatTokens(tokens int) string {
	if tokens < thousand {
		return strconv.Itoa(tokens)
	}
	return fmt.Sprintf("%.1fk", float64(tokens)/thousand)
}

func formatCost(cost float64) string {
	if cost < minDisplayedCost {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

func (m *model) helpView() string {
	help := m.keyHelpLine()
	if m.width > 0 {
		help = foldText(help, m.width)
	}
	return m.st.dim.Render(help)
}

func (m *model) View() string {
	if !m.ready {
		return m.tr.t("tui.common.starting", nil)
	}
	if m.diff.open {
		return m.diffScreen()
	}
	if m.themeScr != nil {
		return m.themeScreenView()
	}
	if m.surface != surfaceChat {
		return m.surfaceScreen()
	}
	body := ""
	if m.vp.Height > 0 {
		body = m.vp.View()
	}
	if side := m.sidebarPane(m.vp.Height); side != nil {
		body = m.joinSidebar(body, side)
	}
	return m.overlayToast(m.st.finish(m.buildFrame().render(body)))
}

func (m *model) busyRow(tab *tab) string {
	elapsedText := ""
	if !tab.startedAt.IsZero() {
		elapsedText = elapsed(time.Since(tab.startedAt))
	}
	tokens := ""
	if tab.tokens > 0 {
		tokens = formatTokens(tab.tokens)
	}
	cost := ""
	if tab.costTotal > 0 {
		cost = formatCost(tab.costTotal)
	}
	return busyLine(m.tr, m.vp.Width, m.st, m.spinFrame(), elapsedText, tokens, cost, m.labelledHint(actCancel, m.tr.t("tui.common.interrupt", nil), chatKeyStack()...))
}
