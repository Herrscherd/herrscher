package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	narrowContext   = 80
	contextNameCols = 16
	contextBarCols  = gaugeWidth
	contextValCols  = 20
	defaultWidth    = 80
)

type contextCategory struct {
	nameKey string
	needKey string
	parts   []contracts.ContextPart
	rest    bool
}

func (c contextCategory) measured() bool {
	return len(c.parts) > 0 || c.rest
}

func (c contextCategory) claims(part contracts.ContextPart) bool {
	for _, own := range c.parts {
		if own == part {
			return true
		}
	}
	return false
}

func injectedTokens(category contextCategory, injected []contracts.InjectedPart) int {
	total := 0
	for _, part := range injected {
		if category.claims(part.Part) || (category.rest && !claimedByAny(part.Part)) {
			total += part.Tokens
		}
	}
	return total
}

func claimedByAny(part contracts.ContextPart) bool {
	for _, category := range contextCategories() {
		if category.claims(part) {
			return true
		}
	}
	return false
}

func contextCategories() []contextCategory {
	return []contextCategory{
		{nameKey: "tui.context.cat_system", needKey: "tui.context.cat_system_need"},
		{nameKey: "tui.context.cat_host", needKey: "tui.context.cat_host_need", rest: true},
		{nameKey: "tui.context.cat_tools", needKey: "tui.context.cat_tools_need"},
		{nameKey: "tui.context.cat_memory", needKey: "tui.context.cat_memory_need", parts: []contracts.ContextPart{contracts.ContextMemory}},
		{nameKey: "tui.context.cat_skills", needKey: "tui.context.cat_skills_need", parts: []contracts.ContextPart{contracts.ContextSkills}},
		{nameKey: "tui.context.cat_files", needKey: "tui.context.cat_files_need"},
		{nameKey: "tui.context.cat_conversation", needKey: "tui.context.cat_conversation_need"},
	}
}

const unavailable = "n/a"

func (m *model) openContext() {
	m.ctxOpen = true
	m.relayout()
}

func (m *model) closeContext() {
	m.ctxOpen = false
	m.relayout()
}

func (m *model) handleContextKey(msg tea.KeyMsg) tea.Cmd {
	chosen := m.stackAction(msg, ctxContext)
	if run, ok := m.tables().context[chosen]; ok {
		return run(m, msg)
	}
	return nil
}

func contextRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
		bind[keyRun](actContextCompact, cmdKey((*model).compactCmd)),
		bind[keyRun](actContextSync, stepKey(func(app *model) { app.syncTabs() })),
		bind[keyRun](actNotifyCycle, cmdKey((*model).cycleNotify)),
		bind[keyRun](actContextClose, stepKey((*model).closeContext)),
	)
}

func (m *model) contextView() string {
	if !m.ctxOpen {
		return ""
	}
	width := m.width
	if width <= 0 {
		width = defaultWidth
	}
	used, limit := m.contextFigures()
	info, _ := m.activeInfo()
	lines := []string{m.contextHeader(), m.contextTotalLine(used, limit, info.AutoCompactPercent, width)}
	for _, category := range contextCategories() {
		lines = append(lines, m.contextCategoryLine(category, limit, width))
	}
	lines = append(lines, m.contextCacheLine(width), m.contextFreeLine(used, limit, width))
	if note := m.lastCompactNote(); note != "" {
		lines = append(lines, m.st.dim.Render(note))
	}
	lines = append(lines, m.st.dim.Render(joinNonEmpty(
		m.labelledHint(actContextCompact, m.tr.t("tui.context.compact", nil), ctxContext),
		m.labelledHint(actContextSync, m.tr.t("tui.context.show_again", nil), ctxContext),
		m.labelledHint(actContextClose, m.tr.t("tui.common.close", nil), ctxContext),
	)))
	for i := range lines {
		lines[i] = cutText(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

func (m *model) contextFigures() (int, int) {
	info, _ := m.activeInfo()
	limit := m.windowOf(info)
	used := 0
	if target := m.tabs[m.active]; target != nil {
		used = m.contextTokens(target)
	}
	return used, limit
}

func (m *model) contextHeader() string {
	info, _ := m.activeInfo()
	name := m.activeSessionName()
	if name == "" {
		name = sessionVerb
	}
	return m.st.dim.Render(m.tr.t("tui.context.title", nil) + "  " + joinNonEmpty(name, info.Vendor, modelLabel(info)))
}

func (m *model) contextTotalLine(used, limit, percent, width int) string {
	ratio := contextRatio(used, limit)
	value := unavailable + "/" + formatLimit(limit)
	pct := unavailable
	if used > 0 {
		value = formatTokens(used) + "/" + formatLimit(limit)
		pct = fmt.Sprintf("%d%%", int(ratio*percentScale))
	}
	bar := gauge(ratio, palierStyle(ratio, m.st), m.st.dim, m.st.glyphs)
	if used <= 0 {
		bar = m.st.dim.Render(strings.Repeat(m.st.glyphs.gaugeEmpty, gaugeWidth))
	}
	return m.contextRow(m.tr.t("tui.context.total", nil), bar, value+" · "+pct, compactThresholdLabel(m.tr, percent), width)
}

func (m *model) contextCategoryLine(category contextCategory, limit, width int) string {
	target := m.tabs[m.active]
	if !category.measured() || target == nil || !target.injectedKnown {
		return m.contextRow(m.tr.t(category.nameKey, nil), m.emptyContextBar(), unavailable, m.tr.t(category.needKey, nil), width)
	}
	tokens := injectedTokens(category, target.injected)
	ratio := contextRatio(tokens, limit)
	bar := barGauge(ratio, palierStyle(ratio, m.st), m.st.dim, m.st.glyphs)
	return m.contextRow(m.tr.t(category.nameKey, nil), bar, estimatedTokens(tokens), m.tr.t("tui.context.host_estimate", nil), width)
}

func estimatedTokens(tokens int) string {
	if tokens <= 0 {
		return "0"
	}
	return "~" + formatTokens(tokens)
}

func (m *model) contextCacheLine(width int) string {
	target := m.tabs[m.active]
	name := m.tr.t("tui.context.cache", nil)
	if target == nil || target.lastPrompt.total() <= 0 {
		return m.contextRow(name, m.emptyContextBar(), unavailable, m.tr.t("tui.context.cache_need", nil), width)
	}
	ratio := target.lastPrompt.cacheShare()
	bar := barGauge(ratio, m.st.text, m.st.dim, m.st.glyphs)
	return m.contextRow(name, bar, fmt.Sprintf("%d%%", int(ratio*percentScale)), m.tr.t("tui.context.cache_note", nil), width)
}

func (m *model) emptyContextBar() string {
	return m.st.dim.Render(strings.Repeat(m.st.glyphs.gaugeEmpty, contextBarCols))
}

func (m *model) contextFreeLine(used, limit, width int) string {
	value := unavailable
	if used > 0 {
		value = formatTokens(max(0, limit-used))
	}
	return m.contextRow(m.tr.t("tui.context.free", nil), m.emptyContextBar(), value, m.tr.t("tui.context.free_note", nil), width)
}

func (m *model) contextRow(name, bar, value, note string, width int) string {
	if width < narrowContext {
		return m.st.dim.Render(padTo(name, contextNameCols)) + m.st.text.Render(value) + "  " + m.st.dim.Render(note)
	}
	return m.st.dim.Render(padTo(name, contextNameCols)) + bar + "  " +
		m.st.text.Render(padTo(value, contextValCols)) + "  " + m.st.dim.Render(note)
}
