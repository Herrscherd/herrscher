package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type notifyMode int

const (
	notifyBoth notifyMode = iota
	notifyTurnEnd
	notifyApproval
)

const notifyModeCount = 3

func (mode notifyMode) label(trans translator) string {
	switch mode {
	case notifyTurnEnd:
		return trans.t("tui.notify.mode_turn_end", nil)
	case notifyApproval:
		return trans.t("tui.notify.mode_approval", nil)
	case notifyBoth:
	}
	return trans.t("tui.notify.mode_both", nil)
}

func (mode notifyMode) wants(kind notifyKind) bool {
	switch mode {
	case notifyBoth:
		return true
	case notifyTurnEnd:
		return kind == kindTurnEnd
	case notifyApproval:
		return kind == kindApproval
	}
	return false
}

type notifyKind int

const (
	kindTurnEnd notifyKind = iota
	kindApproval
)

const (
	toastTTL        = 6 * time.Second
	toastTick       = time.Second
	toastWidth      = 56
	toastBandBelow  = 2 * toastWidth
	toastMinimum    = 24
	toastColumn     = 1
	toastRow        = 2
	toastBorderRows = 2
	toastFrameCols  = 2
	toastChrome     = 4
	toastCorner     = 2
	toastTopFrame   = 8
	bellSequence    = "\a"
	osc9Prefix      = "\x1b]9;"
	osc9Suffix      = "\a"
	osc9Max         = 200
)

type toast struct {
	seq     int
	channel string
	glyph   string
	title   string
	body    []string
	signal  string
	at      time.Time
}

type toastExpiredMsg struct {
	seq int
}

type toastTickMsg struct {
	seq int
}

type focusState struct {
	focused bool
	known   bool
}

func (m *model) cycleNotify() tea.Cmd {
	m.notifyMode = (m.notifyMode + 1) % notifyModeCount
	m.flash = m.tr.t("tui.notify.mode", map[string]any{"Mode": m.notifyMode.label(m.tr)})
	return nil
}

func (m *model) shouldSignal() bool {
	return !m.focus.known || !m.focus.focused
}

func (m *model) notifyTurnEnd(target *tab) tea.Cmd {
	name := m.sessionName(target.channel)
	if name == "" {
		name = target.label
	}
	var duration string
	if !target.startedAt.IsZero() {
		duration = elapsed(time.Since(target.startedAt))
	}
	info := m.sessionInfo(target.channel)
	subject := joinNonEmpty(name, duration, info.Vendor, strings.Join(info.Gateways, "+"))
	return m.announce(kindTurnEnd, m.st.glyphs.success, m.tr.t("tui.notify.turn_done", nil), target.channel, []string{subject})
}

func (m *model) notifyApproval(channel, question string) tea.Cmd {
	info := m.sessionInfo(channel)
	subject := joinNonEmpty(m.sessionName(channel), info.Vendor, strings.Join(info.Gateways, "+"))
	return m.announce(kindApproval, m.st.glyphs.attention, m.tr.t("tui.notify.approval_pending", nil), channel, []string{subject, question})
}

func (m *model) jumpToToast() {
	channel := m.toast.channel
	m.toast = toast{}
	m.activate(channel)
}

func (m *model) announce(kind notifyKind, glyph, title, channel string, body []string) tea.Cmd {
	if !m.notifyMode.wants(kind) {
		return nil
	}
	m.toastSeq++
	m.toast = toast{seq: m.toastSeq, channel: channel, glyph: glyph, title: title, body: body, signal: deliveryLabel(m.tr, m.caps, m.shouldSignal()), at: time.Now()}
	seq := m.toastSeq
	expire := tea.Tick(toastTTL, func(time.Time) tea.Msg { return toastExpiredMsg{seq: seq} })
	return tea.Batch(m.signalCmd(title+": "+joinNonEmpty(body...)), expire, toastTicker(seq))
}

func (m *model) signalCmd(text string) tea.Cmd {
	if !m.shouldSignal() {
		return nil
	}
	return writeSignal(bellOrOSC9(m.caps, text))
}

func deliveryLabel(trans translator, caps Capabilities, signalled bool) string {
	if !signalled {
		return trans.t("tui.notify.delivery_none", nil)
	}
	if caps.OSC9 {
		return trans.t("tui.notify.delivery_osc9", nil)
	}
	return trans.t("tui.notify.delivery_bel", nil)
}

func bellOrOSC9(caps Capabilities, text string) string {
	if caps.OSC9 {
		return osc9Sequence(text)
	}
	return bellSequence
}

func osc9Sequence(text string) string {
	clean := strings.Map(func(char rune) rune {
		if char < ' ' || char == 0x7f {
			return -1
		}
		return char
	}, text)
	return osc9Prefix + ansi.Truncate(clean, osc9Max, "") + osc9Suffix
}

type signalFailedMsg struct {
	err error
}

func writeSignal(sequence string) tea.Cmd {
	return func() tea.Msg {
		if err := writeStdout(sequence); err != nil {
			return signalFailedMsg{err: err}
		}
		return nil
	}
}

func (m *model) handleToastExpired(msg toastExpiredMsg) {
	if msg.seq == m.toast.seq {
		m.toast = toast{}
	}
}

func toastTicker(seq int) tea.Cmd {
	return tea.Tick(toastTick, func(time.Time) tea.Msg { return toastTickMsg{seq: seq} })
}

func (m *model) handleToastTick(msg toastTickMsg) tea.Cmd {
	if m.toast.title == "" || msg.seq != m.toast.seq {
		return nil
	}
	return toastTicker(msg.seq)
}

func (m *model) handleFocus(focused bool) {
	m.focus = focusState{focused: focused, known: true}
}

func (m *model) toastView(rows int) string {
	if m.toast.title == "" {
		return ""
	}
	width := m.toastBoxWidth()
	inner := width - toastChrome
	edge := m.st.dim
	titleStyle := m.st.green
	if m.toast.glyph == m.st.glyphs.attention {
		titleStyle = m.st.warn
	}
	lines := []string{toastTop(edge, titleStyle.Render(m.toast.glyph+" "+m.toast.title), m.st.dim.Render(m.tr.t("tui.notify.ago", map[string]any{"Elapsed": elapsed(time.Since(m.toast.at))})), width)}
	for _, text := range squeezeToastTexts(m.toastTexts(), rows-toastBorderRows, inner) {
		lines = append(lines, toastRowLine(edge, text, inner))
	}
	lines = append(lines, edge.Render("╰"+strings.Repeat("─", width-toastCorner)+"╯"))
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], m.width, "")
	}
	return strings.Join(lines, "\n")
}

func (m *model) toastTexts() []string {
	var texts []string
	for _, text := range m.toast.body {
		if text != "" {
			texts = append(texts, m.st.text.Render(text))
		}
	}
	return append(texts, m.st.dim.Render(joinNonEmpty(m.toast.signal, m.labelledHint(actToastJump, m.tr.t("tui.notify.jump", nil), ctxToast))))
}

func squeezeToastTexts(texts []string, room, inner int) []string {
	if len(texts) <= room {
		return texts
	}
	if room <= 0 {
		return nil
	}
	kept := append([]string(nil), texts[:room]...)
	kept[room-1] = ansi.Truncate(kept[room-1], max(inner-lipgloss.Width(ellipsis), 0), "") + ellipsis
	return kept
}

func (m *model) toastBoxWidth() int {
	if m.width < toastBandBelow {
		return max(toastMinimum, m.width-toastFrameCols)
	}
	return max(toastMinimum, min(toastWidth, m.width-toastFrameCols))
}

func toastTop(edge lipgloss.Style, title, age string, width int) string {
	fill := max(1, width-toastTopFrame-lipgloss.Width(title)-lipgloss.Width(age))
	return edge.Render("╭─ ") + title + edge.Render(" "+strings.Repeat("─", fill)+" ") + age + edge.Render(" ─╮")
}

func toastRowLine(edge lipgloss.Style, text string, inner int) string {
	clipped := ansi.Truncate(text, inner, "…")
	padding := strings.Repeat(" ", max(0, inner-lipgloss.Width(clipped)))
	return edge.Render("│ ") + clipped + padding + edge.Render(" │")
}

func overlayAt(base, block string, row, column int) string {
	if block == "" {
		return base
	}
	lines := strings.Split(base, "\n")
	blockLines := strings.Split(block, "\n")
	for i, piece := range blockLines {
		target := row + i
		if target < 0 || target >= len(lines) {
			continue
		}
		line := lines[target]
		if lipgloss.Width(line) < column {
			line += strings.Repeat(" ", column-lipgloss.Width(line))
		}
		left := ansi.Truncate(line, column, "")
		right := ansi.TruncateLeft(line, column+lipgloss.Width(piece), "")
		lines[target] = left + piece + right
	}
	return strings.Join(lines, "\n")
}

func (m *model) overlayToast(rendered string) string {
	block := m.toastView(m.toastRows())
	if block == "" {
		return rendered
	}
	column := max(0, m.width-lipgloss.Width(firstLine(block))-toastColumn)
	return overlayAt(rendered, withRightGutter(block, m.width-column), toastRow, column)
}

func (m *model) toastRows() int {
	return m.height - toastRow
}

func withRightGutter(block string, width int) string {
	lines := strings.Split(block, "\n")
	for index, line := range lines {
		if gap := width - lipgloss.Width(line); gap > 0 {
			lines[index] = line + strings.Repeat(" ", gap)
		}
	}
	return strings.Join(lines, "\n")
}

func firstLine(block string) string {
	head, _, _ := strings.Cut(block, "\n")
	return head
}

func toastRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(bind[keyRun](actToastJump, stepKey(func(app *model) { app.jumpToToast() })))
}
