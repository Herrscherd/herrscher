package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	panelMinWidth   = 100
	panelWidth      = 40
	panelKeyWidth   = 12
	panelIndent     = 3
	panelNoteStart  = panelIndent + panelKeyWidth
	panelValueWidth = 24
	panelTitle      = "Résumé"
	skillPrivate    = "private"
	scheduleLive    = "live"
	nextPrefix      = "next "
	denyPrefix      = "deny "
	rulePrefix      = "- "
	skillFields     = 3
	scheduleFields  = 6
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
	return sortedKeys(vendorSet), terminalFirst(sortedKeys(gatewaySet))
}

func terminalFirst(gateways []string) []string {
	ordered := slices.DeleteFunc(slices.Clone(gateways), func(gateway string) bool { return gateway == gatewayTerminal })
	if len(ordered) < len(gateways) {
		return slices.Insert(ordered, 0, gatewayTerminal)
	}
	return ordered
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
	vendors, gateways := m.panelFacets()
	rows := []string{m.st.titledRule(m.st.dim, panelTitle, panelWidth)}
	rows = append(rows, m.panelRow("vendeurs", m.st.accent.Render(valueOrNA(strings.Join(vendors, " ")))))
	for index, line := range wrapWords(gateways, panelValueWidth) {
		key := ""
		if index == 0 {
			key = "passerelles"
		}
		rows = append(rows, m.panelRow(key, m.st.violet.Render(line)))
	}
	rows = append(rows,
		m.panelRow("mémoire", m.st.text.Render(unavailable)),
		m.panelRow("skills", m.st.green.Render(m.skillsValue())),
		m.panelNote("écrite par l’agent"),
		m.panelRow("cron", m.st.text.Render(m.cronValue())),
		m.panelRow("politique", m.st.text.Render(m.policyValue())),
		m.panelNote("même règle partout"),
	)
	return rows
}

func (m *model) skillsValue() string {
	skills := m.summary.skills
	if skills == nil {
		return unavailable
	}
	return fmt.Sprintf("%d · %d à approuver", skills.total, skills.pending)
}

func (m *model) cronValue() string {
	cron := m.summary.cron
	if cron == nil {
		return unavailable
	}
	value := fmt.Sprintf("%d %s", cron.live, choosePlural(cron.live, "tour", "tours"))
	if cron.next != "" {
		value += " · " + cron.next
	}
	return value
}

func (m *model) policyValue() string {
	rules := m.summary.rules
	if rules == nil {
		return unavailable
	}
	return fmt.Sprintf("%d %s · %d %s", rules.total, choosePlural(rules.total, "règle", "règles"), rules.deny, choosePlural(rules.deny, "noire", "noires"))
}

func valueOrNA(value string) string {
	if value == "" {
		return unavailable
	}
	return value
}

func wrapWords(words []string, width int) []string {
	var lines []string
	current := ""
	for _, word := range words {
		switch {
		case current == "":
			current = word
		case lipgloss.Width(current)+1+lipgloss.Width(word) <= width:
			current += " " + word
		default:
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return []string{unavailable}
	}
	return lines
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

func joinPanel(left, right []string, leftWidth, height int, separator string) []string {
	left, right = padLines(left, height), padLines(right, height)
	out := make([]string, height)
	for index := range height {
		out[index] = padTo(truncate(left[index], leftWidth), leftWidth) + separator + truncate(right[index], panelWidth)
	}
	return out
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
