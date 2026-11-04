package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	panelMinWidth  = 100
	panelWidth     = 40
	panelKeyWidth  = 12
	panelIndent    = 3
	panelNoteStart = panelIndent + panelKeyWidth
	skillPrivate   = "private"
	scheduleLive   = "live"
	nextPrefix     = "next "
	denyPrefix     = "deny "
	rulePrefix     = "- "
	skillFields    = 3
	scheduleFields = 6
)

type skillCount struct {
	total, pending int
}

type cronCount struct {
	live int
	next string
}

type ruleCount struct {
	total, deny int
}

type panelSummary struct {
	skills *skillCount
	cron   *cronCount
	rules  *ruleCount
}

type summaryMsg panelSummary

func (m *model) summaryRefresh() tea.Cmd {
	if m.surface != surfaceBoard || m.summaryBusy || m.tm == nil {
		return nil
	}
	m.summaryBusy = true
	backend := m.tm
	return func() tea.Msg {
		return summaryMsg{
			skills: parseSkillList(dispatchText(backend, "skill", "list")),
			cron:   parseScheduleList(dispatchText(backend, "schedule", "list")),
			rules:  parseRuleList(dispatchText(backend, "approve", "rule")),
		}
	}
}

func dispatchText(backend Backend, args ...string) string {
	out, err := backend.Dispatch(args)
	if err != nil {
		return ""
	}
	return out
}

func parseSkillList(text string) *skillCount {
	var count skillCount
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < skillFields {
			continue
		}
		count.total++
		if fields[1] == contracts.StateActive && fields[2] == skillPrivate {
			count.pending++
		}
	}
	if count.total == 0 {
		return nil
	}
	return &count
}

func parseScheduleList(text string) *cronCount {
	var count cronCount
	var earliest time.Time
	rows := 0
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < scheduleFields {
			continue
		}
		rows++
		if fields[3] == scheduleLive {
			count.live++
		}
		when, err := time.Parse(time.RFC3339, strings.TrimPrefix(fields[5], nextPrefix))
		if err == nil && (earliest.IsZero() || when.Before(earliest)) {
			earliest = when
		}
	}
	if rows == 0 {
		return nil
	}
	if !earliest.IsZero() {
		count.next = earliest.Local().Format("15:04")
	}
	return &count
}

func parseRuleList(text string) *ruleCount {
	var count ruleCount
	for _, line := range strings.Split(text, "\n") {
		rule, ok := strings.CutPrefix(line, rulePrefix)
		if !ok {
			continue
		}
		count.total++
		if strings.HasPrefix(rule, denyPrefix) {
			count.deny++
		}
	}
	if count.total == 0 {
		return nil
	}
	return &count
}

func (m *model) panelFacets() ([]string, []string) {
	if m.tm == nil {
		return nil, nil
	}
	vendorSet, gatewaySet := make(map[string]bool), make(map[string]bool)
	sessions := m.tm.Sessions()
	for index := range sessions {
		info := &sessions[index]
		if info.Vendor != "" {
			vendorSet[info.Vendor] = true
		}
		for _, gateway := range info.Gateways {
			gatewaySet[gateway] = true
		}
	}
	return sortedKeys(vendorSet), sortedKeys(gatewaySet)
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (m *model) panelLines() []string {
	var rows []string
	vendors, gateways := m.panelFacets()
	if len(vendors) > 0 {
		rows = append(rows, m.panelRow("vendeurs", strings.Join(vendors, " ")))
	}
	if len(gateways) > 0 {
		rows = append(rows, m.panelRow("passerelles", strings.Join(gateways, " ")))
	}
	if skills := m.summary.skills; skills != nil {
		rows = append(rows,
			m.panelRow("skills", fmt.Sprintf("%d · %d à approuver", skills.total, skills.pending)),
			m.panelNote("écrite par l'agent"))
	}
	if cron := m.summary.cron; cron != nil {
		value := fmt.Sprintf("%d %s", cron.live, choosePlural(cron.live, "active", "actives"))
		if cron.next != "" {
			value += " · " + cron.next
		}
		rows = append(rows, m.panelRow("cron", value))
	}
	if rules := m.summary.rules; rules != nil {
		rows = append(rows,
			m.panelRow("politique", fmt.Sprintf("%d %s · %d %s", rules.total, choosePlural(rules.total, "règle", "règles"), rules.deny, choosePlural(rules.deny, "noire", "noires"))),
			m.panelNote("même règle partout"))
	}
	if len(rows) == 0 {
		return nil
	}
	return append([]string{m.st.accent.Render("RÉSUMÉ"), ""}, rows...)
}

func choosePlural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

func (m *model) panelRow(key, value string) string {
	return strings.Repeat(" ", panelIndent) + padTo(m.st.dim.Render(key), panelKeyWidth) + value
}

func (m *model) panelNote(text string) string {
	return strings.Repeat(" ", panelNoteStart) + m.st.dim.Render(text)
}

func (m *model) boardSurface(width, height int) string {
	if width < panelMinWidth {
		return m.boardView(width, height)
	}
	panel := m.panelLines()
	if len(panel) == 0 {
		return m.boardView(width, height)
	}
	boardWidth := width - panelWidth - 1
	board := strings.Split(m.boardView(boardWidth, height), "\n")
	return m.joinPanel(board, panel, boardWidth)
}

func (m *model) joinPanel(left, right []string, leftWidth int) string {
	separator := m.st.dim.Render(m.st.glyphs.divider)
	rows := max(len(left), len(right))
	out := make([]string, 0, rows)
	for index := range rows {
		out = append(out, padTo(truncate(lineAt(left, index), leftWidth), leftWidth)+separator+truncate(lineAt(right, index), panelWidth))
	}
	return strings.Join(out, "\n")
}

func lineAt(lines []string, index int) string {
	if index < len(lines) {
		return lines[index]
	}
	return ""
}

func (m *model) markVisibleSeen() {
	if m.tm == nil {
		return
	}
	for _, name := range m.visibleSessionNames() {
		m.tm.Seen(name)
	}
}

func (m *model) visibleSessionNames() []string {
	var channels []string
	switch m.surface {
	case surfaceChat:
		channels = []string{m.active}
	case surfacePanes:
		channels = m.panes.channels
	case surfaceBoard:
	}

	var names []string
	for _, channel := range channels {
		if name := m.infoForChannel(channel).Name; name != "" {
			names = append(names, name)
		}
	}
	return names
}
