package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	modeManual = "manuel"
	modeEdits  = "edits"
	modePlan   = "plan"
	modeAuto   = "auto"

	modeShortBelow  = 80
	modeStripIndent = "     "
	modeStripSep    = " → "
	planApproved    = "Plan approved. Implement it."
	planKeepNotice  = "Le plan reste ouvert. Dis ce qui change : l'agent le réécrit sans rien toucher."
)

type sessionMode struct {
	host  string
	label string
	short string
	say   string
}

func modeOrder() []string {
	return []string{modeManual, modeEdits, modePlan, modeAuto}
}

func modeSpec(id string) sessionMode {
	switch id {
	case modeEdits:
		return sessionMode{host: "accept-edits", label: "Édits acceptés", short: "Édits", say: "les éditions passent, les commandes demandent encore"}
	case modePlan:
		return sessionMode{host: "plan", label: "Plan", short: "Plan", say: "lecture seule : l'agent propose un plan, tu le valides avant toute écriture"}
	case modeAuto:
		return sessionMode{host: "auto", label: "Auto", short: "Auto", say: "rien ne demande : éditions et commandes passent, sauf ce qu'une règle de refus bloque"}
	}
	return sessionMode{host: "ask", label: "Manuel", short: "Man", say: "chaque édition et chaque commande demande une approbation"}
}

func modeStyle(id string, styles uiStyles) lipgloss.Style {
	switch id {
	case modeEdits:
		return styles.modeEdits
	case modePlan:
		return styles.modePlan
	case modeAuto:
		return styles.modeAuto
	}
	return styles.modeManual
}

func modeIDFromHost(host string) (string, bool) {
	switch host {
	case "ask", "strict":
		return modeManual, true
	case "accept-edits":
		return modeEdits, true
	case "plan":
		return modePlan, true
	case "auto", "bypass":
		return modeAuto, true
	}
	return "", false
}

func modeText(id string, width int, glyphs glyphSet) string {
	spec := modeSpec(id)
	text := spec.label
	if width < modeShortBelow {
		text = spec.short
	}
	if id == modeAuto {
		text = glyphs.warn + " " + text
	}
	return text
}

func nextModeID(id string) string {
	order := modeOrder()
	for index, candidate := range order {
		if candidate == id {
			return order[(index+1)%len(order)]
		}
	}
	return modeManual
}

func modeIDFromArg(arg string) (string, bool) {
	for _, id := range modeOrder() {
		if id == arg {
			return id, true
		}
	}
	return "", false
}

func (t *tab) modeID() string {
	if t.mode == "" {
		return modeManual
	}
	return t.mode
}

type modeLoadedMsg struct {
	channel string
	host    string
}

type modeResultMsg struct {
	channel  string
	previous string
	mode     string
	submit   string
	err      error
}

func (m *model) cycleMode() tea.Cmd {
	tb := m.tabs[m.active]
	if tb == nil {
		return nil
	}
	return m.switchModeCmd(m.active, tb, nextModeID(tb.modeID()), "")
}

func (m *model) switchModeCmd(channel string, tb *tab, mode, submit string) tea.Cmd {
	name := m.sessionName(channel)
	if name == "" {
		m.flash = "aucune session pour changer de mode"
		return nil
	}
	previous := tb.modeID()
	tb.mode = mode
	backend := m.tm
	args := []string{sessionVerb, "set-mode", "--name", name, "--mode", modeSpec(mode).host}
	return func() tea.Msg {
		_, err := backend.Dispatch(args)
		return modeResultMsg{channel: channel, previous: previous, mode: mode, submit: submit, err: err}
	}
}

func (m *model) loadModesCmd() tea.Cmd {
	var cmds []tea.Cmd
	for _, channel := range m.order {
		tb := m.tabs[channel]
		if tb == nil || tb.modeAsked {
			continue
		}
		name := m.sessionName(channel)
		if name == "" {
			continue
		}
		tb.modeAsked = true
		cmds = append(cmds, m.getModeCmd(channel, name))
	}
	return tea.Batch(cmds...)
}

func (m *model) getModeCmd(channel, name string) tea.Cmd {
	backend := m.tm
	args := []string{sessionVerb, "get-mode", "--name", name}
	return func() tea.Msg {
		out, err := backend.Dispatch(args)
		if err != nil {
			return nil
		}
		return modeLoadedMsg{channel: channel, host: strings.TrimSpace(out)}
	}
}

func (m *model) handleModeLoaded(msg modeLoadedMsg) {
	tb := m.tabs[msg.channel]
	if tb == nil || tb.mode != "" {
		return
	}
	if id, ok := modeIDFromHost(msg.host); ok {
		tb.mode = id
	}
}

func (m *model) handleModeClick(msg tea.MouseMsg) (tea.Cmd, bool) {
	tb := m.tabs[m.active]
	if tb == nil || msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil, false
	}
	strip := m.buildFrame()
	if !strip.stripDown || msg.Y != strip.stripRow {
		return nil, false
	}
	for _, hit := range m.modeStripHits(tb, m.innerWidth()) {
		if msg.X < hit.start || msg.X >= hit.end {
			continue
		}
		if hit.id == tb.modeID() {
			return nil, true
		}
		return m.switchModeCmd(m.active, tb, hit.id, ""), true
	}
	return nil, false
}

func (m *model) handleModeResult(msg modeResultMsg) tea.Cmd {
	tb := m.tabs[msg.channel]
	if tb == nil {
		return nil
	}
	if msg.err != nil {
		if tb.mode == msg.mode {
			tb.mode = msg.previous
		}
		tb.appendEntry(entry{role: roleError, text: msg.err.Error()})
		m.syncViewport()
		return nil
	}
	if msg.submit == "" {
		return nil
	}
	return m.submitTo(msg.channel, msg.submit, nil)
}

func (m *model) modeSlash(arg string) tea.Cmd {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return m.cycleMode()
	}
	id, ok := modeIDFromArg(arg)
	if !ok {
		m.flash = "/mode <manuel|edits|plan|auto>"
		return nil
	}
	tb := m.tabs[m.active]
	if tb == nil {
		return nil
	}
	return m.switchModeCmd(m.active, tb, id, "")
}

func (m *model) planSlash(instruction string) tea.Cmd {
	tb := m.tabs[m.active]
	if tb == nil {
		return nil
	}
	return m.switchModeCmd(m.active, tb, modePlan, strings.TrimSpace(instruction))
}

func (m *model) modeChip(tb *tab, width int) string {
	id := tb.modeID()
	chip := modeStyle(id, m.st).Bold(true).Render(modeText(id, width, m.st.glyphs))
	if width < modeShortBelow {
		return chip
	}
	return chip + m.st.dim.Render(" "+m.st.glyphs.shiftTab)
}

type modeHit struct {
	id    string
	start int
	end   int
}

func (m *model) modeStripHits(tb *tab, width int) []modeHit {
	current := tb.modeID()
	column := lipgloss.Width(" " + m.st.glyphs.shiftTab + " ")
	var hits []modeHit
	for index, id := range modeOrder() {
		if index > 0 {
			column += lipgloss.Width(modeStripSep)
		}
		end := column + lipgloss.Width(modeSegmentLabel(id, current, width, m.st.glyphs))
		hits = append(hits, modeHit{id: id, start: column, end: end})
		column = end
	}
	return hits
}

func (m *model) modeStripView(tb *tab, width int) []string {
	current := tb.modeID()
	var segments []string
	for index, id := range modeOrder() {
		if index > 0 {
			segments = append(segments, m.st.dim.Render(modeStripSep))
		}
		segments = append(segments, m.modeSegment(id, current, width))
	}
	line := m.st.dim.Render(" "+m.st.glyphs.shiftTab+" ") + strings.Join(segments, "")
	say := ansi.Truncate(modeStripIndent+modeSpec(current).say, width, "…")
	sayStyle := m.st.dim
	if current == modeAuto {
		sayStyle = m.st.modeAuto
	}
	return []string{ansi.Truncate(line, width, ""), sayStyle.Render(say)}
}

func modeSegmentLabel(id, current string, width int, glyphs glyphSet) string {
	if id == current {
		return " " + modeText(id, width, glyphs) + " "
	}
	return modeText(id, width, glyphs)
}

func (m *model) modeSegment(id, current string, width int) string {
	if id == current {
		return modeStyle(id, m.st).Bold(true).Reverse(true).Render(modeSegmentLabel(id, current, width, m.st.glyphs))
	}
	if id == modeAuto {
		return m.st.modeAuto.Render(modeSegmentLabel(id, current, width, m.st.glyphs))
	}
	return m.st.dim.Render(modeSegmentLabel(id, current, width, m.st.glyphs))
}

type planMenu struct {
	channel string
	sel     int
}

type planOption struct {
	label  string
	say    string
	mode   string
	submit string
}

func planOptions() []planOption {
	return []planOption{
		{label: "Valider et passer en édits acceptés", say: "l'agent écrit sans redemander, les commandes demandent", mode: modeEdits, submit: planApproved},
		{label: "Valider en manuel", say: "chaque édition passera par une approbation", mode: modeManual, submit: planApproved},
		{label: "Continuer à planifier", say: "dire ce qui change dans le plan"},
	}
}

func (m *model) openPlanMenu(channel string) {
	m.planMenu = &planMenu{channel: channel}
	m.applySize()
	m.syncViewport()
}

func (m *model) movePlan(delta int) {
	count := len(planOptions())
	m.planMenu.sel = (m.planMenu.sel + delta + count) % count
}

func (m *model) choosePlan(index int) tea.Cmd {
	menu := m.planMenu
	m.planMenu = nil
	m.applySize()
	m.syncViewport()
	options := planOptions()
	if menu == nil || index < 0 || index >= len(options) {
		return nil
	}
	option := options[index]
	tb := m.tabs[menu.channel]
	if tb == nil {
		return nil
	}
	if option.mode == "" {
		tb.appendEntry(entry{role: roleNotice, text: planKeepNotice})
		m.syncViewport()
		return nil
	}
	return m.switchModeCmd(menu.channel, tb, option.mode, option.submit)
}

func (m *model) handlePlanKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyUp:
		m.movePlan(-1)
	case tea.KeyDown:
		m.movePlan(1)
	case tea.KeyEnter:
		return m.choosePlan(m.planMenu.sel)
	case tea.KeyEsc:
		return m.choosePlan(len(planOptions()) - 1)
	case tea.KeyCtrlC:
		return m.overlayCtrlC()
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			return m.choosePlan(int(msg.Runes[0] - '1'))
		}
	}
	return nil
}

func (m *model) planMenuView() string {
	if m.planMenu == nil {
		return ""
	}
	width := m.innerWidth()
	lines := []string{ansi.Truncate(m.st.accent.Render(m.st.glyphs.tool+" Plan proposé")+m.st.dim.Render(" · rien n'est écrit avant ta validation"), width, "…")}
	for index, option := range planOptions() {
		row := fmt.Sprintf("%d. %s", index+1, option.label)
		if index == m.planMenu.sel {
			lines = append(lines, m.st.accent.Render(m.st.glyphs.cursor+" "+ansi.Truncate(row, width-2, "…")))
		} else {
			lines = append(lines, m.st.dim.Render("  "+ansi.Truncate(row, width-2, "…")))
		}
		lines = append(lines, m.st.dim.Render(ansi.Truncate(modeStripIndent+option.say, width, "…")))
	}
	footer := "↑↓ choisir · ⏎ valider · échap continuer à planifier"
	return strings.Join(append(lines, m.st.dim.Render(ansi.Truncate(footer, width, "…"))), "\n")
}
