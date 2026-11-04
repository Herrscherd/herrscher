package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type modeID uint8

const (
	modeNone modeID = iota
	modeManual
	modeEdits
	modePlan
	modeAuto
)

const (
	hostAsk         = "ask"
	hostStrict      = "strict"
	hostAcceptEdits = "accept-edits"
	hostPlan        = "plan"
	hostAuto        = "auto"
	hostBypass      = "bypass"
	argMode         = "--mode"
	verbSetMode     = "set-mode"
	verbGetMode     = "get-mode"
)

const (
	modeShortBelow  = 80
	modeStripIndent = "     "
	modeStripSep    = " → "
	cursorMarkWidth = 2
	planApproved    = "Plan approved. Implement it."
	planKeepNotice  = "Le plan reste ouvert. Dis ce qui change : l’agent le réécrit sans rien toucher."
)

type sessionMode struct {
	host  string
	label string
	short string
	say   string
}

func modeOrder() []modeID {
	return []modeID{modeManual, modeEdits, modePlan, modeAuto}
}

func (mode modeID) String() string {
	switch mode {
	case modeNone:
		return ""
	case modeManual:
		return "manuel"
	case modeEdits:
		return "edits"
	case modePlan:
		return hostPlan
	case modeAuto:
		return hostAuto
	}
	return strconv.Itoa(int(mode))
}

func modeSpec(mode modeID) sessionMode {
	switch mode {
	case modeEdits:
		return sessionMode{host: hostAcceptEdits, label: "Édits acceptés", short: "Édits", say: "les éditions passent, les commandes demandent encore"}
	case modePlan:
		return sessionMode{host: hostPlan, label: "Plan", short: "Plan", say: "lecture seule : l’agent propose un plan, tu le valides avant toute écriture"}
	case modeAuto:
		return sessionMode{host: hostAuto, label: "Auto", short: "Auto", say: "rien ne demande : éditions et commandes passent, sauf ce qu’une règle de refus bloque"}
	case modeNone, modeManual:
	}
	return sessionMode{host: hostAsk, label: "Manuel", short: "Man", say: "chaque édition et chaque commande demande une approbation"}
}

func modeStyle(mode modeID, styles uiStyles) lipgloss.Style {
	switch mode {
	case modeEdits:
		return styles.modeEdits
	case modePlan:
		return styles.modePlan
	case modeAuto:
		return styles.modeAuto
	case modeNone, modeManual:
	}
	return styles.modeManual
}

func modeIDFromHost(host string) (modeID, bool) {
	switch host {
	case hostAsk, hostStrict:
		return modeManual, true
	case hostAcceptEdits:
		return modeEdits, true
	case hostPlan:
		return modePlan, true
	case hostAuto, hostBypass:
		return modeAuto, true
	}
	return modeNone, false
}

func modeText(mode modeID, width int, glyphs glyphSet) string {
	spec := modeSpec(mode)
	text := spec.label
	if width < modeShortBelow {
		text = spec.short
	}
	if mode == modeAuto {
		text = glyphs.warn + " " + text
	}
	return text
}

func nextModeID(mode modeID) modeID {
	order := modeOrder()
	for index, candidate := range order {
		if candidate == mode {
			return order[(index+1)%len(order)]
		}
	}
	return modeManual
}

func modeIDFromArg(arg string) (modeID, bool) {
	for _, mode := range modeOrder() {
		if mode.String() == arg {
			return mode, true
		}
	}
	return modeNone, false
}

func (target *tab) modeID() modeID {
	if target.mode == modeNone {
		return modeManual
	}
	return target.mode
}

func (target *tab) setMode(mode modeID) {
	target.mode = mode
}

type modeLoadedMsg struct {
	channel string
	host    string
	err     error
}

type modeResultMsg struct {
	channel  string
	previous modeID
	mode     modeID
	submit   string
	err      error
}

func (m *model) cycleMode() tea.Cmd {
	target := m.tabs[m.active]
	if target == nil {
		return nil
	}
	return m.switchModeCmd(m.active, target, nextModeID(target.modeID()), "")
}

func (m *model) switchModeCmd(channel string, target *tab, mode modeID, submit string) tea.Cmd {
	name := m.sessionName(channel)
	if name == "" {
		m.flash = "aucune session pour changer de mode"
		return nil
	}
	previous := target.modeID()
	target.setMode(mode)
	backend := m.tm
	args := []string{sessionVerb, verbSetMode, argName, name, argMode, modeSpec(mode).host}
	return func() tea.Msg {
		_, err := backend.Dispatch(args)
		return modeResultMsg{channel: channel, previous: previous, mode: mode, submit: submit, err: err}
	}
}

func (m *model) loadModesCmd() tea.Cmd {
	var cmds []tea.Cmd
	for _, channel := range m.order {
		target := m.tabs[channel]
		if target == nil || target.modeAsked {
			continue
		}
		name := m.sessionName(channel)
		if name == "" {
			continue
		}
		target.modeAsked = true
		cmds = append(cmds, m.getModeCmd(channel, name))
	}
	return tea.Batch(cmds...)
}

func (m *model) getModeCmd(channel, name string) tea.Cmd {
	backend := m.tm
	args := []string{sessionVerb, verbGetMode, argName, name}
	return func() tea.Msg {
		out, err := backend.Dispatch(args)
		return modeLoadedMsg{channel: channel, host: strings.TrimSpace(out), err: err}
	}
}

func (m *model) handleModeLoaded(msg modeLoadedMsg) {
	target := m.tabs[msg.channel]
	if target == nil || target.mode != modeNone || msg.err != nil {
		return
	}
	if mode, ok := modeIDFromHost(msg.host); ok {
		target.setMode(mode)
	}
}

func (m *model) handleModeClick(msg tea.MouseMsg) (tea.Cmd, bool) {
	target := m.tabs[m.active]
	if target == nil || msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil, false
	}
	strip := m.buildFrame()
	if !strip.stripDown || msg.Y != strip.stripRow {
		return nil, false
	}
	for _, hit := range m.modeStripHits(m.innerWidth()) {
		if msg.X < hit.start || msg.X >= hit.end {
			continue
		}
		if hit.mode == target.modeID() {
			return nil, true
		}
		return m.switchModeCmd(m.active, target, hit.mode, ""), true
	}
	return nil, false
}

func (m *model) handleModeResult(msg modeResultMsg) tea.Cmd {
	target := m.tabs[msg.channel]
	if target == nil {
		return nil
	}
	if msg.err != nil {
		if target.modeID() == msg.mode {
			target.setMode(msg.previous)
		}
		target.appendEntry(entry{role: roleError, text: msg.err.Error()})
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
	mode, ok := modeIDFromArg(arg)
	if !ok {
		m.flash = "/mode <manuel|edits|plan|auto>"
		return nil
	}
	target := m.tabs[m.active]
	if target == nil {
		return nil
	}
	return m.switchModeCmd(m.active, target, mode, "")
}

func (m *model) planSlash(instruction string) tea.Cmd {
	target := m.tabs[m.active]
	if target == nil {
		return nil
	}
	return m.switchModeCmd(m.active, target, modePlan, strings.TrimSpace(instruction))
}

func (m *model) modeChip(target *tab, width int) string {
	mode := target.modeID()
	chip := modeStyle(mode, m.st).Bold(true).Render(modeText(mode, width, m.st.glyphs))
	if width < modeShortBelow {
		return chip
	}
	return chip + m.st.dim.Render(" "+m.st.glyphs.shiftTab)
}

type modeHit struct {
	mode  modeID
	start int
	end   int
}

func (m *model) modeStripHits(width int) []modeHit {
	column := lipgloss.Width(" " + m.st.glyphs.shiftTab + " ")
	var hits []modeHit
	for index, mode := range modeOrder() {
		if index > 0 {
			column += lipgloss.Width(modeStripSep)
		}
		end := column + lipgloss.Width(modeSegmentLabel(mode, width, m.st.glyphs))
		hits = append(hits, modeHit{mode: mode, start: column, end: end})
		column = end
	}
	return hits
}

func (m *model) modeStripView(target *tab, width int) []string {
	current := target.modeID()
	var segments []string
	for index, mode := range modeOrder() {
		if index > 0 {
			segments = append(segments, m.st.dim.Render(modeStripSep))
		}
		segments = append(segments, m.modeSegment(mode, current, width))
	}
	line := m.st.dim.Render(" "+m.st.glyphs.shiftTab+" ") + strings.Join(segments, "")
	say := ansi.Truncate(modeStripIndent+modeSpec(current).say, width, "…")
	sayStyle := m.st.dim
	if current == modeAuto {
		sayStyle = m.st.modeAuto
	}
	return []string{ansi.Truncate(line, width, ""), sayStyle.Render(say)}
}

func modeSegmentLabel(mode modeID, width int, glyphs glyphSet) string {
	return " " + modeText(mode, width, glyphs) + " "
}

func (m *model) modeSegment(mode, current modeID, width int) string {
	if mode == current {
		return modeStyle(mode, m.st).Bold(true).Reverse(true).Render(modeSegmentLabel(mode, width, m.st.glyphs))
	}
	if mode == modeAuto {
		return m.st.modeAuto.Render(modeSegmentLabel(mode, width, m.st.glyphs))
	}
	return m.st.dim.Render(modeSegmentLabel(mode, width, m.st.glyphs))
}

type planMenu struct {
	channel string
	sel     int
}

type planOption struct {
	label  string
	say    string
	mode   modeID
	submit string
}

func planOptions() []planOption {
	return []planOption{
		{label: "Valider et passer en édits acceptés", say: "l’agent écrit sans redemander, les commandes demandent", mode: modeEdits, submit: planApproved},
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
	target := m.tabs[menu.channel]
	if target == nil {
		return nil
	}
	if option.mode == modeNone {
		target.appendEntry(entry{role: roleNotice, text: planKeepNotice})
		m.syncViewport()
		return nil
	}
	return m.switchModeCmd(menu.channel, target, option.mode, option.submit)
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
	lines := []string{ansi.Truncate(m.st.accent.Render(m.st.glyphs.tool+" Plan proposé")+m.st.dim.Render(" · rien n’est écrit avant ta validation"), width, "…")}
	for index, option := range planOptions() {
		row := fmt.Sprintf("%d. %s", index+1, option.label)
		if index == m.planMenu.sel {
			lines = append(lines, m.st.accent.Render(m.st.glyphs.cursor+" "+ansi.Truncate(row, width-cursorMarkWidth, "…")))
		} else {
			lines = append(lines, m.st.dim.Render("  "+ansi.Truncate(row, width-cursorMarkWidth, "…")))
		}
		lines = append(lines, m.st.dim.Render(ansi.Truncate(modeStripIndent+option.say, width, "…")))
	}
	footer := "↑↓ choisir · ⏎ valider · échap continuer à planifier"
	return strings.Join(append(lines, m.st.dim.Render(ansi.Truncate(footer, width, "…"))), "\n")
}
