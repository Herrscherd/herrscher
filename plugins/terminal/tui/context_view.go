package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	narrowContext   = 80
	contextNameCols = 16
	contextBarCols  = 16
	contextValCols  = 20
	defaultWidth    = 80
)

type contextCategory struct {
	nameKey string
	needKey string
}

func contextCategories() []contextCategory {
	return []contextCategory{
		{nameKey: "tui.context.cat_system", needKey: "tui.context.cat_system_need"},
		{nameKey: "tui.context.cat_tools", needKey: "tui.context.cat_tools_need"},
		{nameKey: "tui.context.cat_memory", needKey: "tui.context.cat_memory_need"},
		{nameKey: "tui.context.cat_skills", needKey: "tui.context.cat_skills_need"},
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
		lines = append(lines, m.contextCategoryLine(category, width))
	}
	lines = append(lines, m.contextFreeLine(used, limit, width))
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

func (m *model) contextCategoryLine(category contextCategory, width int) string {
	return m.contextRow(m.tr.t(category.nameKey, nil), m.st.dim.Render(strings.Repeat(m.st.glyphs.gaugeEmpty, contextBarCols)), unavailable, m.tr.t(category.needKey, nil), width)
}

func (m *model) contextFreeLine(used, limit, width int) string {
	value := unavailable
	if used > 0 {
		value = formatTokens(max(0, limit-used))
	}
	return m.contextRow(m.tr.t("tui.context.free", nil), m.st.dim.Render(strings.Repeat(m.st.glyphs.gaugeEmpty, contextBarCols)), value, m.tr.t("tui.context.free_note", nil), width)
}

func (m *model) contextRow(name, bar, value, note string, width int) string {
	if width < narrowContext {
		return m.st.dim.Render(padTo(name, contextNameCols)) + m.st.text.Render(value) + "  " + m.st.dim.Render(note)
	}
	return m.st.dim.Render(padTo(name, contextNameCols)) + bar + "  " +
		m.st.text.Render(padTo(value, contextValCols)) + "  " + m.st.dim.Render(note)
}
