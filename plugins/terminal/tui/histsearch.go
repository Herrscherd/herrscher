package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *model) historyFooter() string {
	return joinNonEmpty(
		m.labelledHint(actHistorySearch, m.tr.t("tui.common.history", nil), chatKeyStack()...),
		m.labelledHint(actHistoryAccept, m.tr.t("tui.search.take", nil), ctxHistorySearch),
		m.labelledHint(actHistoryCancel, m.tr.t("tui.common.close", nil), ctxHistorySearch),
	)
}

func (m *model) openHistSearch() {
	m.histOpen = true
	m.histQuery = ""
	m.histSel = 0
}

func (m *model) closeHistSearch() {
	m.histOpen = false
	m.histQuery = ""
	m.histSel = 0
}

func (m *model) histMatches() []string {
	newestFirst := make([]string, 0, len(m.history))
	for i := len(m.history) - 1; i >= 0; i-- {
		newestFirst = append(newestFirst, m.history[i])
	}
	return rankFuzzy(m.histQuery, newestFirst)
}

func (m *model) moveHistSel(delta int) {
	count := len(m.histMatches())
	m.histSel += delta
	if m.histSel >= count {
		m.histSel = count - 1
	}
	if m.histSel < 0 {
		m.histSel = 0
	}
}

func (m *model) handleHistSearchKey(msg tea.KeyMsg) tea.Cmd {
	action := m.stackAction(msg, ctxHistorySearch)
	if run, ok := m.tables().histSearch[action]; ok {
		return run(m, msg)
	}
	if action == "" {
		m.typeHistQuery(msg)
	}
	return nil
}

func histSearchRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
		bind[keyRun](actHistoryCancel, stepKey((*model).closeHistSearch)),
		bind[keyRun](actHistoryAccept, stepKey(func(app *model) {
			if matches := app.histMatches(); len(matches) > 0 {
				app.input.SetValue(matches[app.histSel])
				app.input.CursorEnd()
				app.resizeComposer()
			}
			app.closeHistSearch()
		})),
		bind[keyRun](actSelectPrevious, stepKey(func(app *model) { app.moveHistSel(-1) })),
		bind[keyRun](actSelectNext, stepKey(func(app *model) { app.moveHistSel(1) })),
		bind[keyRun](actHistoryNextMatch, stepKey(func(app *model) { app.moveHistSel(1) })),
	)
}

func (m *model) typeHistQuery(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyBackspace:
		if runes := []rune(m.histQuery); len(runes) > 0 {
			m.histQuery = string(runes[:len(runes)-1])
			m.histSel = 0
		}
	case tea.KeySpace:
		m.histQuery += " "
		m.histSel = 0
	case tea.KeyRunes:
		if !msg.Alt {
			m.histQuery += string(msg.Runes)
			m.histSel = 0
		}
	}
}

func (m *model) histView() string {
	matches := m.histMatches()
	if len(matches) == 0 {
		return m.st.dim.Render("  (" + m.tr.t("tui.palette.no_match", nil) + ")")
	}
	visible := m.overlayRows()
	start, _ := paletteWindow(len(matches), m.histSel, visible)
	end := start + visible
	if end > len(matches) {
		end = len(matches)
	}
	rows := make([]overlayRow, 0, end-start)
	for _, text := range matches[start:end] {
		line := oneLine(text)
		row := overlayRow{label: line}
		if hit, ok := fuzzyMatch(m.histQuery, line); ok {
			row.hot = hit.positions
		}
		rows = append(rows, row)
	}
	footer := m.historyFooter()
	if q := strings.TrimSpace(m.histQuery); q != "" {
		footer = "« " + q + " » · " + footer
	}
	return floatingList(rows, m.histSel-start, footer, m.overlayWidth(), m.st)
}
