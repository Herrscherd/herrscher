package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	narrowContext   = 80
	contextNameCols = 16
	contextBarCols  = 12
	contextValCols  = 14
	defaultWidth    = 80
	autoCompactPct  = 95
)

type contextCategory struct {
	name string
	need string
}

func contextCategories() []contextCategory {
	return []contextCategory{
		{name: "système", need: "taille du prompt système, envoyé à chaque tour"},
		{name: "outils et MCP", need: "taille des schémas d'outils, par serveur MCP"},
		{name: "mémoire", need: "taille du bloc mémoire injecté dans le tour"},
		{name: categorySkills, need: "taille des descriptions de skills chargées dans le contexte"},
		{name: "fichiers lus", need: "tokens de chaque fichier lu par l'agent"},
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
	if msg.Type == tea.KeyCtrlC {
		return m.overlayCtrlC()
	}
	switch msg.String() {
	case "c":
		return m.compactCmd()
	case "k":
		m.syncTabs()
		return nil
	case "n":
		return m.cycleNotify()
	case "z", keyEsc:
		m.closeContext()
	}
	return nil
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
	lines := []string{m.contextHeader(), m.contextTotalLine(used, limit, width)}
	for _, category := range contextCategories() {
		lines = append(lines, m.contextCategoryLine(category, width))
	}
	lines = append(lines, m.contextFreeLine(used, limit, width))
	if note := m.lastCompactNote(); note != "" {
		lines = append(lines, m.st.dim.Render(note))
	}
	lines = append(lines, m.st.dim.Render("c compacter · k réafficher · échap fermer"))
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "")
	}
	return strings.Join(lines, "\n")
}

func (m *model) contextFigures() (int, int) {
	info, _ := m.activeInfo()
	limit := contextLimit(info.Vendor)
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

func (m *model) contextTotalLine(used, limit, width int) string {
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
	return m.contextRow("total", bar, value+" · "+pct, "compacte seul à "+fmt.Sprintf("%d%%", autoCompactPct), width)
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
		m.st.text.Render(padTo(value, contextValCols)) + m.st.dim.Render(note)
}
