package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	paneMax            = 4
	paneSingleBelow    = 80
	paneGridWidth      = 120
	paneGridHeight     = 30
	resumePreviewLines = 3
	paneColumns        = 2
	paneGridSide       = 2
	paneHalves         = 2
	paneSepWidth       = 2
	paneInset          = 4
	paneBoxChrome      = 3
	paneGaugeCells     = 6
	paneStackRows      = 2
	paneMinCellHeight  = paneBoxChrome + 3
)

const (
	paneNavKeys         = "Tab volet suivant · clic viser"
	paneApprovalKeysFmt = "o s r approbation de "
)

type paneState struct {
	channels []string
	focus    int
	zoom     bool
}

type paneLayout struct {
	cols     int
	rows     int
	capacity int
	single   bool
}

func paneGeometry(width, height, bodyHeight int, zoom bool) paneLayout {
	single := paneLayout{cols: 1, rows: 1, capacity: 1, single: true}
	switch {
	case zoom:
		return single
	case width < paneSingleBelow:
		if bodyHeight >= paneStackRows*paneMinCellHeight {
			return paneLayout{cols: 1, rows: paneStackRows, capacity: paneStackRows}
		}
		return single
	case width >= paneGridWidth && height >= paneGridHeight:
		return paneLayout{cols: paneGridSide, rows: paneGridSide, capacity: paneGridSide * paneGridSide}
	default:
		return paneLayout{cols: paneColumns, rows: 1, capacity: paneColumns}
	}
}

func (m *model) paneLayout() paneLayout {
	top, bottom := m.surfaceChrome()
	return paneGeometry(m.width, m.height, m.surfaceHeight(top, bottom), m.panes.zoom)
}

func (m *model) openPanes() {
	m.surface = surfacePanes
	m.refreshOverviews()
	m.panes = paneState{}
	rows := m.boardCandidates()
	for index := range rows {
		info := rows[index].info
		if info.Archived || len(m.panes.channels) == paneMax {
			continue
		}
		m.panes.channels = append(m.panes.channels, info.ChannelID)
		m.ensureTab(info.ChannelID)
	}
	m.activate(m.focusedChannel())
	m.relayout()
}

func (m *model) movePane(delta int) {
	count := len(m.panes.channels)
	if count == 0 {
		return
	}
	m.focusPane((m.panes.focus + delta + count) % count)
}

func (m *model) focusPane(index int) {
	m.panes.focus = index
	m.activate(m.focusedChannel())
}

func (m *model) focusedChannel() string {
	if m.panes.focus < 0 || m.panes.focus >= len(m.panes.channels) {
		return ""
	}
	return m.panes.channels[m.panes.focus]
}

func (m *model) paneFocusName() string {
	channel := m.focusedChannel()
	if channel == "" {
		return ""
	}
	return m.infoForChannel(channel).Name
}

func (m *model) paneWindow(layout paneLayout) (int, int) {
	if layout.single {
		return m.panes.focus, m.panes.focus + 1
	}
	start := (m.panes.focus / layout.capacity) * layout.capacity
	end := start + layout.capacity
	if end > len(m.panes.channels) {
		end = len(m.panes.channels)
	}
	return start, end
}

func paneWidths(width, cols int) []int {
	if cols == 1 {
		return []int{width}
	}
	left := (width - paneSepWidth) / paneHalves
	return []int{left, width - paneSepWidth - left}
}

func (m *model) surfaceChrome() ([]string, []string) {
	width := m.innerWidth()
	switch m.surface {
	case surfaceBoard:
		return m.railLines(), slices.Concat([]string{m.st.rule(m.st.dim, width), m.boardFooter(width)}, m.statusZone())
	case surfacePanes:
		header := m.paneHeader(width)
		return slices.Concat(m.railLines(), []string{header}), slices.Concat([]string{m.paneFooter(width)}, m.composerZone(), m.statusZone())
	case surfaceChat:
	}
	return nil, nil
}

func (m *model) surfaceHeight(top, bottom []string) int {
	return max(m.height-len(top)-len(bottom), 0)
}

func (m *model) surfaceScreen() string {
	width := m.innerWidth()
	top, bottom := m.surfaceChrome()
	height := m.surfaceHeight(top, bottom)
	var body []string
	switch m.surface {
	case surfaceBoard:
		body = m.boardScreenBody(width, height)
	case surfacePanes:
		body = m.paneBody(width, height)
	case surfaceChat:
		return ""
	}
	return strings.Join(slices.Concat(top, padLines(body, height), bottom), "\n")
}

func (m *model) paneHeader(width int) string {
	left := m.st.violet.Bold(true).Render(" Vue en volets") + m.st.dim.Render("  Tab volet suivant · clic pour viser")
	return cutText(joinRow(width, left, m.paneCounts()), width)
}

func (m *model) paneFooter(width int) string {
	keys := paneNavKeys
	if name, ok := m.paneApproval(); ok {
		keys += footerSeparator + paneApprovalKeysFmt + name
	}
	return m.st.dim.Render(foldText(" "+keys, width))
}

func (m *model) paneApproval() (string, bool) {
	target := m.tabs[m.focusedChannel()]
	if target == nil || !target.pendingApproval {
		return "", false
	}
	if name := m.infoForChannel(target.channel).Name; name != "" {
		return name, true
	}
	return target.label, true
}

func (m *model) paneCounts() string {
	var working, waiting, unread, finished int
	var cost float64
	for _, channel := range m.panes.channels {
		row := m.sessionRow(channel)
		cost += row.overview.Cost
		switch row.overview.State {
		case overviewWaiting:
			waiting++
		case overviewWorking:
			working++
		}
		if row.unread > 0 {
			unread++
		}
		if row.group == groupReview || row.finished {
			finished++
		}
	}
	parts := []string{
		m.st.accent.Render(fmt.Sprintf("%d au travail", working)),
		m.st.warn.Render(fmt.Sprintf("%d approbation", waiting)),
		m.st.accent.Render(fmt.Sprintf("%d non lus", unread)),
		m.st.green.Render(fmt.Sprintf("%d fini", finished)),
		m.st.dim.Render(formatCost(cost)),
	}
	return strings.Join(parts, m.st.dim.Render(" · ")) + " "
}

func (m *model) paneHidden(layout paneLayout) int {
	start, end := m.paneWindow(layout)
	return len(m.panes.channels) - (end - start)
}

func (m *model) paneCellHeight(layout paneLayout, height int) int {
	return max(height-m.paneHiddenReserve(layout), 0) / layout.rows
}

func (m *model) paneHiddenReserve(layout paneLayout) int {
	if m.paneHidden(layout) > 0 {
		return 1
	}
	return 0
}

func (m *model) paneHiddenLine(hidden int) string {
	noun := "volets"
	if hidden == 1 {
		noun = "volet"
	}
	return m.st.dim.Render(fmt.Sprintf(" +%d %s · Tab pour les voir", hidden, noun))
}

func (m *model) paneBody(width, height int) []string {
	if len(m.panes.channels) == 0 {
		return padLines([]string{m.st.dim.Render("aucune session vivante")}, height)
	}
	layout := m.paneLayout()
	start, end := m.paneWindow(layout)
	cellsArea := max(height-m.paneHiddenReserve(layout), 0)
	cellHeight := m.paneCellHeight(layout, height)
	widths := paneWidths(width, layout.cols)
	separator := strings.Repeat(" ", paneSepWidth)
	var lines []string
	for row := range layout.rows {
		cells := make([][]string, layout.cols)
		for col := range layout.cols {
			index := start + row*layout.cols + col
			if index < end {
				cells[col] = m.paneBox(index, widths[col], cellHeight)
			} else {
				cells[col] = padLines(nil, cellHeight)
			}
		}
		for line := range cellHeight {
			parts := make([]string, 0, layout.cols)
			for col := range cells {
				parts = append(parts, padTo(cells[col][line], widths[col]))
			}
			lines = append(lines, strings.Join(parts, separator))
		}
	}
	body := padLines(lines, cellsArea)
	if hidden := m.paneHidden(layout); hidden > 0 {
		body = append(body, m.paneHiddenLine(hidden))
	}
	return padLines(body, height)
}

func (m *model) paneBox(index, width, height int) []string {
	if height < paneBoxChrome || width < paneInset {
		return padLines(nil, height)
	}
	channel := m.panes.channels[index]
	row := m.sessionRow(channel)
	focused := index == m.panes.focus
	style := m.st.dim
	nameStyle := m.st.text
	switch {
	case focused:
		style = m.st.accent
		nameStyle = m.st.accent.Bold(true)
	case row.overview.State == overviewWaiting:
		style = m.st.warn
	}
	mark, markStyle := m.sessionMark(&row)
	titleParts := []string{markStyle.Render(mark) + " " + nameStyle.Render(row.overview.Name)}
	if row.info.Vendor != "" {
		titleParts = append(titleParts, m.st.dim.Render(" · "+row.info.Vendor))
	}
	boxes := m.st.boxes(m.caps.Boxes)
	lines := []string{boxEdge([2]string{boxes.tl, boxes.tr}, titleParts, m.paneStateLabel(&row), width, style, boxes.h)}
	inner := height - paneBoxChrome
	side := style.Render(boxes.v)
	body := padLines(tailLines(m.paneTranscript(m.tabs[channel], width-paneInset), inner), inner)
	for _, line := range body {
		lines = append(lines, side+" "+padTo(cutText(line, width-paneInset), width-paneInset)+" "+side)
	}
	ratio := contextRatio(row.info.ContextTokens, contextLimit(row.info.Vendor))
	footLeft := m.st.dim.Render("ctx ") + m.gaugeCells(ratio, paneGaugeCells, identityPaint) +
		m.tierStyle(ratio).Render(" "+contextPercent(row.info.ContextTokens, contextLimit(row.info.Vendor))) +
		m.st.dim.Render(" · "+formatCost(row.overview.Cost))
	footRight := gatewayStyle(m.st, strings.Join(row.overview.Gateways, "+")).Render(strings.Join(row.overview.Gateways, "+"))
	foot := cutText(joinRow(width-paneInset, footLeft, footRight), width-paneInset)
	lines = append(lines, side+" "+padTo(foot, width-paneInset)+" "+side, style.Render(boxes.bl+strings.Repeat(boxes.h, width-composerBorderCols)+boxes.br))
	return lines
}

func identityPaint(style lipgloss.Style) lipgloss.Style {
	return style
}

func (m *model) paneStateLabel(row *boardRow) string {
	switch {
	case row.overview.State == overviewWaiting:
		return m.st.warn.Render("approbation")
	case row.unread > 0:
		return m.st.accent.Render(fmt.Sprintf("%d non lus", row.unread))
	case row.group == groupReview || row.finished:
		return m.st.green.Render("fini " + overviewAge(row.overview.LastTs, time.Now()))
	case row.overview.State == overviewWorking:
		return m.st.dim.Render(overviewAge(row.overview.LastTs, time.Now()))
	}
	return m.st.dim.Render("au repos")
}

func (m *model) paneTranscript(channelTab *tab, width int) []string {
	if channelTab == nil {
		return nil
	}
	var out []string
	for index := range channelTab.entries {
		item := channelTab.entries[index]
		if item.role == roleTool {
			out = append(out, m.paneToolRow(item, width))
			continue
		}
		out = append(out, strings.Split(renderEntry(item, width, m.displayMode()), "\n")...)
	}
	return out
}

func (m *model) paneToolRow(item entry, width int) string {
	display := m.displayMode()
	family, target := toolVerb(strings.Split(item.text, "\n")[0])
	name := ""
	if family != "" {
		name = toolName(strings.Split(item.text, "\n")[0])
	}
	mark := familyStyle(display.st, family)
	stateMark := display.st.green.Render(display.st.glyphs.success)
	switch {
	case item.failed:
		mark = display.st.red
		stateMark = display.st.red.Render(display.st.glyphs.failure)
	case item.live:
		stateMark = display.st.spinner.Render(display.spin)
	}
	right := stateMark
	if item.span > 0 || item.live {
		right = display.st.dim.Render(formatSpan(item.span)) + " " + stateMark
	}
	room := width - lipgloss.Width(right) - 1
	if room < 1 {
		return cutText(right, width)
	}
	left := cutText(toolLeft(mark, family, name, target, display), room)
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	return left + strings.Repeat(" ", gap) + right
}

func (m *model) infoForChannel(channel string) contracts.SessionInfo {
	if m.tm == nil {
		return contracts.SessionInfo{}
	}
	sessions := m.tm.Sessions()
	index := slices.IndexFunc(sessions, func(info contracts.SessionInfo) bool { return info.ChannelID == channel })
	if index < 0 {
		return contracts.SessionInfo{}
	}
	return sessions[index]
}

func (m *model) handlePanesKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.Type {
	case tea.KeyCtrlP, tea.KeyEsc:
		m.closeSurface()
	case tea.KeyTab:
		m.movePane(1)
	case tea.KeyShiftTab:
		m.movePane(-1)
	case tea.KeyCtrlW:
		m.panes.zoom = !m.panes.zoom
	case tea.KeyLeft, tea.KeyRight:
		if m.input.Value() != "" || !m.paneLayout().single {
			return nil, false
		}
		if msg.Type == tea.KeyLeft {
			m.movePane(-1)
		} else {
			m.movePane(1)
		}
	case tea.KeyEnter:
		return m.sendFromPane(), true
	case tea.KeyCtrlC:
		return m.overlayCtrlC(), true
	default:
		return nil, false
	}
	return nil, true
}

func (m *model) sendFromPane() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	channel := m.focusedChannel()
	if text == "" || channel == "" {
		return nil
	}
	m.input.Reset()
	m.recordHistory(text)
	return tea.Batch(m.submitTo(channel, text, nil), m.ensureSpin())
}

func (m *model) paneCellAt(screenX, screenY int) (int, bool) {
	top, bottom := m.surfaceChrome()
	if screenY < len(top) {
		return 0, false
	}
	layout := m.paneLayout()
	start, end := m.paneWindow(layout)
	cellHeight := max(m.paneCellHeight(layout, m.surfaceHeight(top, bottom)), 1)
	row := (screenY - len(top)) / cellHeight
	col := 0
	if layout.cols > 1 {
		widths := paneWidths(m.innerWidth(), layout.cols)
		if screenX >= widths[0]+paneSepWidth {
			col = 1
		}
	}
	index := start + row*layout.cols + col
	return index, index < end && row < layout.rows
}

func (m *model) handlePaneClick(msg tea.MouseMsg) bool {
	if m.surface != surfacePanes || m.mouseFree || msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return false
	}
	if index, ok := m.paneCellAt(msg.X, msg.Y); ok {
		m.focusPane(index)
	}
	return true
}
