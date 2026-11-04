package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const brand = "──◂ HERRSCHER ▸──"

const (
	railName       = "herrscher"
	railHints      = "^P agents · ^T tab"
	railTabGap     = 2
	railEdgeGap    = 2
	railStripFloor = 20
	railFoldLimit  = 5
)

var railNameLimits = []int{0, 12, 8, railFoldLimit}

type railCell struct {
	text   string
	width  int
	active bool
}

func (m *model) railLines() []string {
	width := m.innerWidth()
	prefix := m.st.text.Bold(true).Render(railName) + strings.Repeat(" ", railEdgeGap)
	suffix := m.st.dim.Render(railHints)
	if width-lipgloss.Width(prefix)-lipgloss.Width(suffix) < railStripFloor {
		suffix = ""
	}
	if width-lipgloss.Width(prefix) < railStripFloor {
		prefix = ""
	}
	reserve := 0
	if suffix != "" {
		reserve = railEdgeGap
	}
	strip, start, span := m.railStrip(width - lipgloss.Width(prefix) - lipgloss.Width(suffix) - reserve)
	row := prefix + strip
	if suffix != "" {
		row = joinRow(width, row, suffix)
	}
	lead := lipgloss.Width(prefix) + start
	rule := m.st.dim.Render(strings.Repeat(ruleChar, lead)) +
		m.st.accent.Render(strings.Repeat(glyphRailMark, span)) +
		m.st.dim.Render(strings.Repeat(ruleChar, max(0, width-lead-span)))
	return []string{truncate(row, width), rule}
}

func (m *model) railChannels() []string {
	var out []string
	for _, channel := range m.order {
		if m.tabs[channel] != nil {
			out = append(out, channel)
		}
	}
	return out
}

func (m *model) railStrip(room int) (string, int, int) {
	channels := m.railChannels()
	if len(channels) == 0 || room < 1 {
		return "", 0, 0
	}
	for _, limit := range railNameLimits {
		cells := m.railCells(channels, limit)
		if railCellsWidth(cells) <= room {
			return joinRail(cells)
		}
	}
	return m.railFold(channels, room)
}

func (m *model) railFold(channels []string, room int) (string, int, int) {
	cells := m.railCells(channels, railFoldLimit)
	active := 0
	for index, cell := range cells {
		if cell.active {
			active = index
		}
	}
	fits := func(from, to int) bool {
		return railCellsWidth(cells[from:to+1])+railFoldWidth(len(cells)-(to-from+1)) <= room
	}
	if !fits(active, active) {
		text := truncate(cells[active].text, room)
		return text, 0, lipgloss.Width(text)
	}
	from, to := active, active
	for grown := true; grown; {
		grown = false
		if to+1 < len(cells) && fits(from, to+1) {
			to++
			grown = true
		}
		if from > 0 && fits(from-1, to) {
			from--
			grown = true
		}
	}
	strip, start, span := joinRail(cells[from : to+1])
	if hidden := len(cells) - (to - from + 1); hidden > 0 {
		strip += strings.Repeat(" ", railTabGap) + m.st.dim.Render("+"+strconv.Itoa(hidden))
	}
	return strip, start, span
}

func railFoldWidth(hidden int) int {
	if hidden < 1 {
		return 0
	}
	return railTabGap + 1 + len(strconv.Itoa(hidden))
}

func railCellsWidth(cells []railCell) int {
	total := 0
	for index, cell := range cells {
		total += cell.width
		if index > 0 {
			total += railTabGap
		}
	}
	return total
}

func joinRail(cells []railCell) (string, int, int) {
	parts := make([]string, 0, len(cells))
	start, span, pos := 0, 0, 0
	for index, cell := range cells {
		if index > 0 {
			pos += railTabGap
		}
		if cell.active {
			start, span = pos, cell.width
		}
		parts = append(parts, cell.text)
		pos += cell.width
	}
	return strings.Join(parts, strings.Repeat(" ", railTabGap)), start, span
}

func (m *model) railCells(channels []string, limit int) []railCell {
	cells := make([]railCell, 0, len(channels))
	for _, channel := range channels {
		cells = append(cells, m.railCell(channel, limit))
	}
	return cells
}

func (m *model) railCell(channel string, limit int) railCell {
	tab := m.tabs[channel]
	name := tab.label
	if limit > 0 {
		name = shortenName(name, limit)
	}
	if channel == m.active {
		text := m.st.accent.Render(glyphSelected) + " " + m.st.accent.Bold(true).Underline(true).Render(name)
		return railCell{text: text, width: lipgloss.Width(text), active: true}
	}
	marker, style := m.railMarker(channel, tab)
	text := style.Render(marker) + " " + m.st.dim.Render(name)
	if tab.unreadCount > 0 {
		text += " " + m.st.accent.Render(strconv.Itoa(tab.unreadCount))
	}
	return railCell{text: text, width: lipgloss.Width(text)}
}

func (m *model) railMarker(channel string, tab *tab) (string, lipgloss.Style) {
	switch {
	case m.choice != nil && m.choice.Channel == channel:
		return glyphAttention, m.st.warn.Bold(true)
	case tab.busy:
		return m.spinFrame(), m.st.spinner
	case tab.unreadCount > 0:
		return glyphUnreadMark, m.st.accent
	case tab.finished:
		return glyphDone, m.st.green
	}
	return glyphOpen, m.st.dim
}

func shortenName(name string, limit int) string {
	if lipgloss.Width(name) <= limit {
		return name
	}
	return ansi.Truncate(name, limit-1, "") + "…"
}

func joinRow(width int, left, right string) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}
