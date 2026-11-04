package tui

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
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
	overviewWaiting  = "waiting"
	overviewWorking  = "working"
	overviewReview   = "review"
	overviewArchived = "archived"
)

const (
	boardListWidth      = 80
	boardNameWidth      = 18
	boardVendorWidth    = 15
	boardGatewayWidth   = 17
	boardWhatIndent     = 5
	boardFooterIndent   = 2
	boardAgentIndent    = 5
	boardAgentRows      = 3
	boardAgentNameWidth = 9
	boardGaugeCells     = 5
	boardDetailCells    = 12
	boardPercentWidth   = 4
	boardDetailRows     = 13
	boardHeaderRows     = 1
	boardSubtitle       = "terminal et Discord ensemble"
	boardNoMatch        = "   (aucune session ne correspond)"
	boardEmpty          = "  aucune session"
	boardArchivedHint   = "/resume pour les reprendre avec aperçu"
	gatewayTerminal     = "terminal"
	hintOpenApproval    = "ouvrir l’approbation"
	hintReviewTurn      = "relire le diff du tour"
	hintOpenPane        = "ouvrir · ⌃P en volet"
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
	unread   int
	finished bool
	agents   []liveAgent
}

type boardState struct {
	query string
	idx   int
	rows  []boardRow
}

type rowPaint func(lipgloss.Style) lipgloss.Style

// ParseSessionOverviews reads the JSON list of overviews, an empty text meaning none.
func ParseSessionOverviews(raw string) ([]SessionOverview, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out []SessionOverview
	err := json.Unmarshal([]byte(raw), &out)
	return out, err
}

func overviewGroup(state string) boardGroupKey {
	switch state {
	case overviewWaiting:
		return groupWaiting
	case overviewWorking:
		return groupWorking
	case overviewReview:
		return groupReview
	}
	return groupDone
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

func newBoardRow(overview SessionOverview, info contracts.SessionInfo, channelTab *tab) boardRow {
	row := boardRow{overview: overview, info: info, group: overviewGroup(overview.State)}
	if channelTab != nil {
		row.unread = channelTab.unreadCount
		row.finished = channelTab.finished
		row.agents = channelTab.agents
	}
	return row
}

func (m *model) sessionRow(channel string) boardRow {
	info := m.infoForChannel(channel)
	return newBoardRow(m.overviewFor(info.Name), info, m.tabs[channel])
}

func (m *model) archivedCount() int {
	count := 0
	for index := range m.overviews {
		if m.overviews[index].State == overviewArchived {
			count++
		}
	}
	return count
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
		if !ok || info.ChannelID == "" || info.Archived || overview.State == overviewArchived {
			continue
		}
		rows = append(rows, newBoardRow(*overview, info, m.tabs[info.ChannelID]))
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
		if m.board.query != "" {
			m.setBoardQuery("")
		} else {
			m.closeSurface()
		}
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

func (m *model) boardScreenBody(width, height int) []string {
	if width < panelMinWidth {
		return m.boardList(width, height)
	}
	listWidth := min(boardListWidth, width-panelWidth-1)
	return joinPanel(m.boardList(listWidth, height), m.boardPanel(), listWidth, height, m.st.dim.Render(m.st.glyphs.divider))
}

func (m *model) boardList(width, height int) []string {
	lines, cursor := m.boardBlocks(width)
	archived := m.archivedCount()
	reserved := 0
	if archived > 0 {
		reserved = 2
	}
	body := padLines(windowLines(lines, cursor, height-boardHeaderRows-reserved), height-boardHeaderRows-reserved)
	out := append([]string{m.boardHeader(width)}, body...)
	if archived > 0 {
		hint := fmt.Sprintf("   archivées : %d · %s", archived, boardArchivedHint)
		out = append(out, "", cutText(m.st.dim.Render(hint), width))
	}
	return padLines(out, height)
}

func (m *model) boardHeader(width int) string {
	line := m.st.accent.Bold(true).Render(" Sessions et agents") +
		m.st.dim.Render("  "+boardSubtitle+" · filtre ") +
		m.st.text.Render(m.board.query) +
		m.st.accent.Render(m.st.glyphs.caret)
	if m.board.query == "" {
		line += m.st.dim.Render(" tape pour filtrer")
	}
	return cutText(line, width)
}

func (m *model) boardBlocks(width int) ([]string, int) {
	if len(m.board.rows) == 0 {
		if m.board.query != "" {
			return []string{m.st.dim.Render(boardNoMatch)}, 0
		}
		return []string{m.st.dim.Render(boardEmpty)}, 0
	}
	var lines []string
	cursor := 0
	var last boardGroupKey
	for index := range m.board.rows {
		row := &m.board.rows[index]
		if row.group != last {
			last = row.group
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, m.boardGroupHeader(row.group, m.groupCount(row.group), width))
		}
		selected := index == m.board.idx
		if selected {
			cursor = len(lines)
		}
		lines = append(lines, m.boardRowLines(row, selected, width)...)
	}
	return lines, cursor
}

func (m *model) groupCount(group boardGroupKey) int {
	count := 0
	for index := range m.board.rows {
		if m.board.rows[index].group == group {
			count++
		}
	}
	return count
}

func (m *model) groupStyle(group boardGroupKey) lipgloss.Style {
	switch group {
	case groupWorking:
		return m.st.accent
	case groupWaiting:
		return m.st.warn
	case groupReview:
		return m.st.green
	case groupDone:
	}
	return m.st.dim
}

func (m *model) boardGroupHeader(group boardGroupKey, count, width int) string {
	head := m.groupStyle(group).Bold(true).Render(" "+strings.ToUpper(groupLabel(group))) + m.st.dim.Render(fmt.Sprintf(" %d ", count))
	fill := max(0, width-lipgloss.Width(head))
	return cutText(head+m.st.dim.Render(strings.Repeat(m.st.glyphs.ruleChar, fill)), width)
}

func (m *model) paintFor(selected bool) rowPaint {
	if !selected {
		return func(style lipgloss.Style) lipgloss.Style { return style }
	}
	overlay := m.st.overlaySel
	return func(style lipgloss.Style) lipgloss.Style { return style.Inherit(overlay) }
}

func (m *model) boardRowLines(row *boardRow, selected bool, width int) []string {
	paint := m.paintFor(selected)
	cursor := "   "
	if selected {
		cursor = " " + m.st.glyphs.prompt + " "
	}
	mark, markStyle := m.sessionMark(row)
	nameStyle := m.st.text
	switch {
	case selected:
		nameStyle = m.st.strong
	case row.group == groupDone:
		nameStyle = m.st.dim
	}
	gateway := strings.Join(row.overview.Gateways, "+")
	head := paint(m.st.accent.Bold(true)).Render(cursor) +
		paint(markStyle).Render(mark) +
		paint(m.st.dim).Render(" ") +
		paint(nameStyle).Render(padTo(cutText(row.overview.Name, boardNameWidth), boardNameWidth)) +
		paint(m.st.text).Render(padTo(cutText(row.overview.Vendor, boardVendorWidth), boardVendorWidth)) +
		paint(gatewayStyle(m.st, gateway)).Render(padTo(cutText(gateway, boardGatewayWidth), boardGatewayWidth))
	first := head + paint(m.st.dim).Render(cutText(sessionWhere(row.info), max(0, width-lipgloss.Width(head))))

	ratio := contextRatio(row.info.ContextTokens, contextLimit(row.info.Vendor))
	right := paint(m.st.dim).Render(overviewAge(row.overview.LastTs, time.Now())+" · ") +
		paint(m.st.warn).Render(formatCost(row.overview.Cost)) +
		paint(m.st.dim).Render(" ") +
		m.gaugeCells(ratio, boardGaugeCells, paint) +
		paint(m.tierStyle(ratio)).Render(fmt.Sprintf(" %*s ", boardPercentWidth, contextPercent(row.info.ContextTokens, contextLimit(row.info.Vendor))))
	what := strings.ReplaceAll(row.overview.LastText, "\n", " ")
	second := paint(m.st.dim).Render(strings.Repeat(" ", boardWhatIndent)) +
		paint(m.sessionTone(row)).Render(cutText(what, width-boardWhatIndent-lipgloss.Width(right)-1))
	gap := max(1, width-lipgloss.Width(second)-lipgloss.Width(right))
	second += paint(m.st.dim).Render(strings.Repeat(" ", gap)) + right
	lines := []string{cutText(first, width), cutText(second, width)}
	return append(lines, m.boardAgentLines(row, width)...)
}

func (m *model) boardAgentLines(row *boardRow, width int) []string {
	shown := min(len(row.agents), boardAgentRows)
	hidden := len(row.agents) - shown
	lines := make([]string, 0, shown+1)
	for index := range shown {
		branch := m.st.glyphs.branchMid
		if hidden == 0 && index == shown-1 {
			branch = m.st.glyphs.branchEnd
		}
		glyph, glyphStyle := m.boardAgentGlyph(row.agents[index])
		line := m.st.dim.Render(strings.Repeat(" ", boardAgentIndent)+branch+" ") + glyphStyle.Render(glyph) + " " +
			m.st.text.Render(padTo(cutText(oneLine(row.agents[index].Name), boardAgentNameWidth), boardAgentNameWidth))
		line += m.st.dim.Render(cutText(oneLine(row.agents[index].Kind), max(0, width-lipgloss.Width(line))))
		lines = append(lines, cutText(line, width))
	}
	if hidden > 0 {
		lines = append(lines, cutText(m.st.dim.Render(fmt.Sprintf("%s%s +%d", strings.Repeat(" ", boardAgentIndent), m.st.glyphs.branchEnd+" ", hidden)), width))
	}
	return lines
}

func (m *model) boardAgentGlyph(agent liveAgent) (string, lipgloss.Style) {
	switch agent.State {
	case stateDone:
		return m.st.glyphs.success, m.st.green.Bold(true)
	case stateFailed, stateError, stateTimeout:
		return m.st.glyphs.failure, m.st.red.Bold(true)
	}
	return m.spinFrame(), m.st.spinner
}

func gatewayStyle(styles uiStyles, gateway string) lipgloss.Style {
	if gateway == gatewayTerminal {
		return styles.dim
	}
	return styles.violet
}

func sessionWhere(info contracts.SessionInfo) string {
	if info.Project != "" {
		return info.Project
	}
	if info.Dir != "" {
		return filepath.Base(info.Dir)
	}
	return ""
}

func (m *model) sessionMark(row *boardRow) (string, lipgloss.Style) {
	switch {
	case row.overview.State == overviewWaiting:
		return m.st.glyphs.attention, m.st.warn.Bold(true)
	case row.overview.State == overviewWorking:
		return m.spinFrame(), m.st.spinner
	case row.unread > 0:
		return m.st.glyphs.unread, m.st.accent.Bold(true)
	case row.group == groupReview || row.finished:
		return m.st.glyphs.success, m.st.green.Bold(true)
	}
	return m.st.glyphs.open, m.st.dim
}

func (m *model) sessionTone(row *boardRow) lipgloss.Style {
	switch {
	case row.overview.State == overviewWaiting:
		return m.st.warn
	case row.unread > 0:
		return m.st.accent
	case row.group == groupReview:
		return m.st.green
	}
	return m.st.dim
}

func (m *model) tierStyle(ratio float64) lipgloss.Style {
	switch {
	case ratio >= ctxAlarm:
		return m.st.red
	case ratio >= ctxWarn:
		return m.st.warn
	}
	return m.st.accent
}

func (m *model) gaugeCells(ratio float64, cells int, paint rowPaint) string {
	filled := max(0, min(cells, int(math.Round(ratio*float64(cells)))))
	return paint(m.tierStyle(ratio)).Render(strings.Repeat(m.st.glyphs.gaugeFull, filled)) +
		paint(m.st.dim).Render(strings.Repeat(m.st.glyphs.gaugeEmpty, cells-filled))
}

func (m *model) boardFooter(width int) string {
	keys := fitSegments([]string{
		hintChoose,
		m.st.glyphs.enter + " ouvrir",
		"lettres filtrer",
		m.st.glyphs.erase + " effacer",
		"échap vider",
		"⌃P en volets",
	}, width-boardFooterIndent)
	sessions, vendors, gateways := m.boardTotals()
	counts := fmt.Sprintf("%d %s · %d %s · %d %s ",
		sessions, choosePlural(sessions, "session", "sessions"),
		vendors, choosePlural(vendors, "vendeur", "vendeurs"),
		gateways, choosePlural(gateways, "passerelle", "passerelles"))
	return cutText(joinRow(width, m.st.dim.Render(" "+keys), m.st.dim.Render(counts)), width)
}

func (m *model) boardTotals() (int, int, int) {
	vendors := make(map[string]bool)
	gateways := make(map[string]bool)
	for index := range m.board.rows {
		overview := &m.board.rows[index].overview
		if overview.Vendor != "" {
			vendors[overview.Vendor] = true
		}
		for _, gateway := range overview.Gateways {
			gateways[gateway] = true
		}
	}
	return len(m.board.rows), len(vendors), len(gateways)
}

func (m *model) boardPanel() []string {
	var detail []string
	if index := m.board.idx; index >= 0 && index < len(m.board.rows) {
		detail = m.boardDetail(&m.board.rows[index])
	}
	return append(padLines(detail, boardDetailRows), m.panelLines()...)
}

func (m *model) boardDetail(row *boardRow) []string {
	mark, markStyle := m.sessionMark(row)
	gateway := strings.Join(row.overview.Gateways, "+")
	ratio := contextRatio(row.info.ContextTokens, contextLimit(row.info.Vendor))
	return []string{
		" " + markStyle.Render(mark) + " " + m.st.strong.Render(row.overview.Name),
		strings.Repeat(" ", panelIndent) + m.sessionTone(row).Render(row.overview.LastText),
		"",
		m.panelRow("vendeur", m.st.text.Render(row.overview.Vendor)),
		m.panelRow("passerelle", gatewayStyle(m.st, gateway).Render(gateway)),
		m.panelRow("où", m.st.text.Render(sessionWhere(row.info))),
		m.panelRow("coût", m.st.warn.Render(formatCost(row.overview.Cost))),
		m.panelRow("contexte", m.gaugeCells(ratio, boardDetailCells, func(style lipgloss.Style) lipgloss.Style { return style })+
			m.tierStyle(ratio).Render(" "+contextPercent(row.info.ContextTokens, contextLimit(row.info.Vendor)))),
		"",
		strings.Repeat(" ", panelIndent) + m.st.dim.Render(m.st.glyphs.enter+" "+m.boardHint(row)),
	}
}

func (m *model) boardHint(row *boardRow) string {
	switch {
	case row.overview.State == overviewWaiting:
		return hintOpenApproval
	case row.group == groupReview:
		return hintReviewTurn
	}
	return hintOpenPane
}

func (m *model) overviewFor(name string) SessionOverview {
	index := slices.IndexFunc(m.overviews, func(overview SessionOverview) bool { return overview.Name == name })
	if index < 0 {
		return SessionOverview{}
	}
	return m.overviews[index]
}

func overviewAge(lastTs string, now time.Time) string {
	started, err := time.Parse(time.RFC3339Nano, lastTs)
	if err != nil {
		return "-"
	}
	return formatDuration(now.Sub(started))
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
