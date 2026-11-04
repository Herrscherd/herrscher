package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// SessionOverview is one session as the overview endpoint reports it.
type SessionOverview struct {
	Name     string   `json:"name"`
	Vendor   string   `json:"vendor"`
	Gateways []string `json:"gateways"`
	State    string   `json:"state"`
	Cost     float64  `json:"cost"`
	LastTs   string   `json:"last_ts"`
	LastText string   `json:"last_text"`
}

const (
	overviewWaiting = "waiting"
	overviewWorking = "working"
	overviewReview  = "review"
)

const (
	boardAgeWidth   = 6
	boardCostWidth  = 8
	boardMarkWidth  = 2
	boardGap        = 2
	boardTextMin    = 16
	boardNameMax    = 24
	boardVendorMax  = 10
	boardGatewayMax = 12
	boardFooterRows = 1
)

type boardGroupKey uint8

const (
	groupWorking boardGroupKey = iota + 1
	groupWaiting
	groupReview
	groupDone
)

type boardGroup struct {
	key   boardGroupKey
	label string
}

func boardGroups() []boardGroup {
	return []boardGroup{
		{groupWorking, "Au travail"},
		{groupWaiting, "Attend une réponse"},
		{groupReview, "À relire"},
		{groupDone, "Fini"},
	}
}

type surface int

const (
	surfaceChat surface = iota
	surfaceBoard
	surfacePanes
)

type boardRow struct {
	overview SessionOverview
	info     contracts.SessionInfo
	group    boardGroupKey
}

type boardState struct {
	query string
	idx   int
	rows  []boardRow
}

// ParseSessionOverviews reads the JSON list of overviews, an empty text meaning none.
func ParseSessionOverviews(raw string) ([]SessionOverview, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out []SessionOverview
	err := json.Unmarshal([]byte(raw), &out)
	return out, err
}

func overviewGroup(state string, unread bool) boardGroupKey {
	switch {
	case state == overviewWaiting:
		return groupWaiting
	case state == overviewWorking:
		return groupWorking
	case unread || state == overviewReview:
		return groupReview
	default:
		return groupDone
	}
}

func groupRank(key boardGroupKey) int {
	groups := boardGroups()
	for index, group := range groups {
		if group.key == key {
			return index
		}
	}
	return len(groups)
}

func groupLabel(key boardGroupKey) string {
	for _, group := range boardGroups() {
		if group.key == key {
			return group.label
		}
	}
	return ""
}

func (m *model) refreshOverviews() {
	if m.tm == nil {
		return
	}
	raw, err := m.tm.Overview()
	if err != nil {
		m.flash = "aperçu indisponible : " + err.Error()
		return
	}
	overviews, err := ParseSessionOverviews(raw)
	if err != nil {
		m.flash = "aperçu illisible : " + err.Error()
		return
	}
	m.overviews = overviews
}

func (m *model) boardCandidates() []boardRow {
	if m.tm == nil {
		return nil
	}
	byName := make(map[string]contracts.SessionInfo)
	sessions := m.tm.Sessions()
	for index := range sessions {
		byName[sessions[index].Name] = sessions[index]
	}
	rows := make([]boardRow, 0, len(m.overviews))
	for index := range m.overviews {
		overview := &m.overviews[index]
		info, ok := byName[overview.Name]
		if !ok || info.ChannelID == "" {
			continue
		}
		tab := m.tabs[info.ChannelID]
		unread := tab != nil && tab.unread
		rows = append(rows, boardRow{overview: *overview, info: info, group: overviewGroup(overview.State, unread)})
	}
	sort.SliceStable(rows, func(left, right int) bool {
		if leftRank, rightRank := groupRank(rows[left].group), groupRank(rows[right].group); leftRank != rightRank {
			return leftRank < rightRank
		}
		return rows[left].overview.LastTs > rows[right].overview.LastTs
	})
	return rows
}

func boardText(row boardRow) string {
	return strings.Join(append([]string{row.overview.Name, row.overview.Vendor}, row.overview.Gateways...), " ")
}

func (m *model) rebuildBoard() {
	m.board.rows = fuzzyFilter(m.boardCandidates(), m.board.query, boardText)
	m.board.idx = clampTo(m.board.idx, len(m.board.rows))
}

func (m *model) openBoard() tea.Cmd {
	m.surface = surfaceBoard
	m.board = boardState{}
	m.refreshOverviews()
	m.rebuildBoard()
	m.relayout()
	return m.summaryRefresh()
}

func (m *model) closeSurface() {
	m.surface = surfaceChat
	m.input.Placeholder = "type a message…"
	m.relayout()
}

func (m *model) refreshSurface() {
	switch m.surface {
	case surfaceBoard:
		m.refreshOverviews()
		m.rebuildBoard()
	case surfacePanes:
		m.refreshOverviews()
	case surfaceChat:
	}
}

func (m *model) setBoardQuery(query string) {
	m.board.query = query
	m.board.idx = 0
	m.rebuildBoard()
}

func (m *model) moveBoard(delta int) {
	m.board.idx = clampTo(m.board.idx+delta, len(m.board.rows))
}

func (m *model) handleBoardKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.closeSurface()
	case tea.KeyCtrlP:
		m.openPanes()
	case tea.KeyUp:
		m.moveBoard(-1)
	case tea.KeyDown:
		m.moveBoard(1)
	case tea.KeyEnter:
		return m.openBoardRow()
	case tea.KeyBackspace:
		runes := []rune(m.board.query)
		if len(runes) > 0 {
			m.setBoardQuery(string(runes[:len(runes)-1]))
		}
	case tea.KeySpace:
		m.setBoardQuery(m.board.query + " ")
	case tea.KeyRunes:
		m.setBoardQuery(m.board.query + string(msg.Runes))
	case tea.KeyCtrlC:
		return m.overlayCtrlC()
	}
	return nil
}

func (m *model) openBoardRow() tea.Cmd {
	if m.board.idx < 0 || m.board.idx >= len(m.board.rows) {
		return nil
	}
	info := m.board.rows[m.board.idx].info
	m.surface = surfaceChat
	m.relayout()
	return m.openChannel(info)
}

func (m *model) boardView(width, height int) string {
	lines := []string{m.st.accent.Render("Sessions") + m.st.dim.Render(fmt.Sprintf(" · %d", len(m.board.rows)))}
	lines = append(lines, m.boardFilterLine())
	layout := boardLayoutFor(m.board.rows, width)
	cursorLine := 0
	var last boardGroupKey
	for index := range m.board.rows {
		row := &m.board.rows[index]
		if row.group != last {
			last = row.group
			lines = append(lines, m.st.accent.Render(groupLabel(row.group)))
		}
		if index == m.board.idx {
			cursorLine = len(lines)
		}
		lines = append(lines, m.boardRowLine(row, index == m.board.idx, width, layout))
	}
	if len(m.board.rows) == 0 {
		lines = append(lines, m.st.dim.Render("  aucune session"))
	}
	body := windowLines(lines, cursorLine, height-boardFooterRows)
	footer := m.st.dim.Render("↑↓ choisir · ⏎ ouvrir · ⌃P volets · échap fermer")
	return strings.Join(padLines(append(body, truncate(footer, width)), height), "\n")
}

func (m *model) boardFilterLine() string {
	if m.board.query == "" {
		return m.st.dim.Render("filtre : tapez pour filtrer")
	}
	return m.st.dim.Render("filtre : ") + m.board.query
}

func (m *model) boardRowLine(row *boardRow, selected bool, width int, layout boardLayout) string {
	prefix := "  "
	if selected {
		prefix = m.st.accent.Render(m.st.glyphs.cursor) + " "
	}
	cells := []string{m.groupMark(row.group), padTo(truncate(row.overview.Name, layout.name), layout.name), padTo(truncate(row.overview.Vendor, layout.vendor), layout.vendor)}
	if layout.showGateway {
		cells = append(cells, padTo(truncate(strings.Join(row.overview.Gateways, "+"), layout.gateway), layout.gateway))
	}
	cells = append(cells, padTo(overviewAge(row.overview.LastTs, time.Now()), boardAgeWidth))
	if layout.showCost {
		cells = append(cells, padTo(formatCost(row.overview.Cost), boardCostWidth))
	}
	text := strings.ReplaceAll(row.overview.LastText, "\n", " ")
	line := prefix + strings.Join(cells, strings.Repeat(" ", boardGap)) + strings.Repeat(" ", boardGap) + m.st.dim.Render(truncate(text, width-layout.fixedWidth()))
	return truncate(line, width)
}

func (m *model) groupMark(group boardGroupKey) string {
	switch group {
	case groupWorking:
		return padTo(m.st.spinner.Render(m.st.glyphs.tool), boardMarkWidth)
	case groupWaiting:
		return padTo(m.st.warn.Render(m.st.glyphs.waiting), boardMarkWidth)
	case groupReview:
		return padTo(m.st.accent.Render(m.st.glyphs.review), boardMarkWidth)
	case groupDone:
		return padTo(m.st.dim.Render(m.st.glyphs.success), boardMarkWidth)
	}
	return padTo("", boardMarkWidth)
}

type boardLayout struct {
	name, vendor, gateway int
	showGateway, showCost bool
}

func (l boardLayout) fixedWidth() int {
	width := boardMarkWidth + boardGap + l.name + boardGap + l.vendor + boardGap + boardAgeWidth + boardGap
	if l.showGateway {
		width += l.gateway + boardGap
	}
	if l.showCost {
		width += boardCostWidth + boardGap
	}
	return width
}

func boardLayoutFor(rows []boardRow, width int) boardLayout {
	layout := boardLayout{
		name:    widestColumn(rows, func(r *boardRow) string { return r.overview.Name }, boardNameMax),
		vendor:  widestColumn(rows, func(r *boardRow) string { return r.overview.Vendor }, boardVendorMax),
		gateway: widestColumn(rows, func(r *boardRow) string { return strings.Join(r.overview.Gateways, "+") }, boardGatewayMax),
	}
	layout.showGateway, layout.showCost = true, true
	if layout.fixedWidth()+boardTextMin <= width {
		return layout
	}
	layout.showCost = false
	if layout.fixedWidth()+boardTextMin <= width {
		return layout
	}
	layout.showGateway = false
	return layout
}

func widestColumn(rows []boardRow, text func(*boardRow) string, limit int) int {
	widest := 0
	for index := range rows {
		if w := lipgloss.Width(text(&rows[index])); w > widest {
			widest = w
		}
	}
	if widest > limit {
		return limit
	}
	return widest
}

func overviewAge(lastTs string, now time.Time) string {
	started, err := time.Parse(time.RFC3339Nano, lastTs)
	if err != nil {
		return "-"
	}
	return formatDuration(now.Sub(started))
}

func (m *model) overviewFor(name string) SessionOverview {
	index := slices.IndexFunc(m.overviews, func(overview SessionOverview) bool { return overview.Name == name })
	if index < 0 {
		return SessionOverview{}
	}
	return m.overviews[index]
}

func clampTo(index, count int) int {
	if index >= count {
		index = count - 1
	}
	if index < 0 {
		index = 0
	}
	return index
}

func windowLines(lines []string, cursor, height int) []string {
	if height < 1 {
		return nil
	}
	start := 0
	if cursor >= height {
		start = cursor - height + 1
	}
	end := start + height
	if end > len(lines) {
		end = len(lines)
	}
	return append([]string(nil), lines[start:end]...)
}

func padLines(lines []string, height int) []string {
	if len(lines) > height {
		return lines[:max(height, 0)]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func tailLines[T any](lines []T, count int) []T {
	if count <= 0 {
		return nil
	}
	if len(lines) > count {
		return lines[len(lines)-count:]
	}
	return lines
}
