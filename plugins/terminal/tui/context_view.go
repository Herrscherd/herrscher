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
	name string
	need string
}

func contextCategories() []contextCategory {
	return []contextCategory{
		{name: "système", need: "taille du prompt système, envoyé à chaque tour"},
		{name: "outils et MCP", need: "taille des schémas d’outils, par serveur MCP"},
		{name: "mémoire", need: "taille du bloc mémoire injecté dans le tour"},
		{name: categorySkills, need: "taille des descriptions de skills chargées dans le contexte"},
		{name: "fichiers lus", need: "tokens de chaque fichier lu par l’agent"},
		{name: categoryConversation, need: "tokens des tours encore gardés, par tour"},
	}
}

const (
	categorySkills       = "skills"
	categoryConversation = "conversation"
)

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

func contextRuns() map[keyAction]keyRun {
	return dispatchTable(func(set func(keyAction, keyRun)) {
		set(actInterrupt, cmdKey((*model).overlayCtrlC))
		set(actContextCompact, cmdKey((*model).compactCmd))
		set(actContextSync, stepKey(func(app *model) { app.syncTabs() }))
		set(actNotifyCycle, cmdKey((*model).cycleNotify))
		set(actContextClose, stepKey((*model).closeContext))
	})
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
		m.labelledHint(actContextCompact, "compacter", ctxContext),
		m.labelledHint(actContextSync, "réafficher", ctxContext),
		m.labelledHint(actContextClose, "fermer", ctxContext),
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
	return m.st.dim.Render("Contexte  " + joinNonEmpty(name, info.Vendor, modelLabel(info)))
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
	return m.contextRow("total", bar, value+" · "+pct, compactThresholdLabel(percent), width)
}

func (m *model) contextCategoryLine(category contextCategory, width int) string {
	return m.contextRow(category.name, m.st.dim.Render(strings.Repeat(m.st.glyphs.gaugeEmpty, contextBarCols)), unavailable, category.need, width)
}

func (m *model) contextFreeLine(used, limit, width int) string {
	value := unavailable
	if used > 0 {
		value = formatTokens(max(0, limit-used))
	}
	return m.contextRow("libre", m.st.dim.Render(strings.Repeat(m.st.glyphs.gaugeEmpty, contextBarCols)), value, "place avant que la fenêtre soit pleine", width)
}

func (m *model) contextRow(name, bar, value, note string, width int) string {
	if width < narrowContext {
		return m.st.dim.Render(padTo(name, contextNameCols)) + m.st.text.Render(value) + "  " + m.st.dim.Render(note)
	}
	return m.st.dim.Render(padTo(name, contextNameCols)) + bar + "  " +
		m.st.text.Render(padTo(value, contextValCols)) + "  " + m.st.dim.Render(note)
}
