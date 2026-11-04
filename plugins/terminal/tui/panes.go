package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	paneMax            = 4
	paneSingleBelow    = 80
	paneGridWidth      = 120
	paneGridHeight     = 30
	resumePreviewLines = 3
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

func paneGeometry(width, height int, zoom bool) paneLayout {
	switch {
	case zoom || width < paneSingleBelow:
		return paneLayout{cols: 1, rows: 1, capacity: 1, single: true}
	case width >= paneGridWidth && height >= paneGridHeight:
		return paneLayout{cols: 2, rows: 2, capacity: 4}
	default:
		return paneLayout{cols: 2, rows: 1, capacity: 2}
	}
}

func (m *model) paneLayout() paneLayout {
	return paneGeometry(m.width, m.height, m.panes.zoom)
}

func (m *model) openPanes() {
	m.surface = surfacePanes
	m.refreshOverviews()
	m.panes = paneState{}
	for _, row := range m.boardCandidates() {
		if row.info.Archived || len(m.panes.channels) == paneMax {
			continue
		}
		m.panes.channels = append(m.panes.channels, row.info.ChannelID)
		m.ensureTab(row.info.ChannelID)
	}
	m.input.Placeholder = "Le message part dans le volet en surbrillance"
	m.relayout()
}

func (m *model) movePane(delta int) {
	count := len(m.panes.channels)
	if count == 0 {
		return
	}
	m.panes.focus = (m.panes.focus + delta + count) % count
}

func (m *model) focusedChannel() string {
	if m.panes.focus < 0 || m.panes.focus >= len(m.panes.channels) {
		return ""
	}
	return m.panes.channels[m.panes.focus]
}

func (m *model) focusedTab() *tab {
	return m.tabs[m.focusedChannel()]
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

const paneSepWidth = 1

func paneWidths(width, cols int) []int {
	if cols == 1 {
		return []int{width}
	}
	left := (width - paneSepWidth) / 2
	return []int{left, width - paneSepWidth - left}
}

func (m *model) paneView(width, height int) string {
	footer := m.st.dim.Render("Tab volet suivant · o s r approbation · ⌃W agrandir · ⌃P quitter")
	if len(m.panes.channels) == 0 {
		lines := []string{m.st.dim.Render("aucune session vivante")}
		return strings.Join(padLines(append(lines, truncate(footer, width)), height), "\n")
	}
	layout := m.paneLayout()
	start, end := m.paneWindow(layout)
	bodyHeight := height - boardFooterRows
	cellHeight := bodyHeight / layout.rows
	widths := paneWidths(width, layout.cols)
	var lines []string
	for row := 0; row < layout.rows; row++ {
		cells := make([][]string, layout.cols)
		for col := 0; col < layout.cols; col++ {
			index := start + row*layout.cols + col
			if index < end {
				cells[col] = m.paneCell(index, widths[col], cellHeight)
			} else {
				cells[col] = padLines(nil, cellHeight)
			}
		}
		for line := 0; line < cellHeight; line++ {
			parts := make([]string, 0, layout.cols)
			for col := range cells {
				parts = append(parts, padTo(cells[col][line], widths[col]))
			}
			lines = append(lines, strings.Join(parts, m.st.dim.Render(m.st.glyphs.divider)))
		}
	}
	return strings.Join(padLines(append(lines, truncate(footer, width)), height), "\n")
}

func (m *model) paneCell(index, width, height int) []string {
	channel := m.panes.channels[index]
	tb := m.tabs[channel]
	info := m.infoForChannel(channel)
	ov := m.overviewFor(info.Name)
	marker := "  "
	nameStyle := m.st.text
	if index == m.panes.focus {
		marker = m.st.accent.Render(m.st.glyphs.cursor) + " "
		nameStyle = m.st.accent
	}
	parts := []string{nameStyle.Render(info.Name), info.Vendor, strings.Join(info.Gateways, "+"), renderContext(info.ContextTokens, info.Vendor, m.st), formatCost(ov.Cost)}
	header := marker + m.st.dim.Render(joinNonEmptyDot(parts))
	lines := []string{truncate(header, width), m.st.rule(m.st.dim, width)}
	reserved := len(lines)
	body := tailLines(m.paneTranscript(tb, width), height-reserved)
	lines = append(lines, body...)
	return padLines(lines, height)
}

func joinNonEmptyDot(parts []string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " · ")
}

func (m *model) paneTranscript(tb *tab, width int) []string {
	if tb == nil {
		return nil
	}
	var out []string
	for _, item := range tb.entries {
		out = append(out, strings.Split(renderEntry(item, width, m.displayMode()), "\n")...)
	}
	return out
}

func (m *model) infoForChannel(channel string) contracts.SessionInfo {
	if m.tm == nil {
		return contracts.SessionInfo{}
	}
	for _, info := range m.tm.Sessions() {
		if info.ChannelID == channel {
			return info
		}
	}
	return contracts.SessionInfo{}
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

func (m *model) paneCellAt(x, y int) (int, bool) {
	top := len(m.buildFrame().above)
	if y < top {
		return 0, false
	}
	layout := m.paneLayout()
	start, end := m.paneWindow(layout)
	cellHeight := (m.vp.Height - boardFooterRows) / layout.rows
	row := (y - top) / max(cellHeight, 1)
	col := 0
	if layout.cols > 1 {
		widths := paneWidths(m.vp.Width, layout.cols)
		if x > widths[0] {
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
		m.panes.focus = index
	}
	return true
}

func (m *model) surfaceBody() string {
	switch m.surface {
	case surfaceBoard:
		return m.boardSurface(m.vp.Width, m.vp.Height)
	case surfacePanes:
		return m.paneView(m.vp.Width, m.vp.Height)
	default:
		return ""
	}
}
