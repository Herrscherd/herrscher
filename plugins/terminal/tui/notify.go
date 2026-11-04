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

func (mode notifyMode) label() string {
	switch mode {
	case notifyTurnEnd:
		return "fin de tour"
	case notifyApproval:
		return "approbation"
	case notifyBoth:
	}
	return "fin de tour et approbation"
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
	toastTTL       = 6 * time.Second
	toastTick      = time.Second
	toastWidth     = 56
	toastBandBelow = 2 * toastWidth
	toastMinimum   = 24
	toastColumn    = 1
	toastRow       = 2
	toastFrameCols = 2
	toastChrome    = 4
	toastCorner    = 2
	toastTopFrame  = 8
	bellSequence   = "\a"
	osc9Prefix     = "\x1b]9;"
	osc9Suffix     = "\a"
	osc9Max        = 200
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
	m.flash = "notifications : " + m.notifyMode.label()
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
	return m.announce(kindTurnEnd, m.st.glyphs.success, "Tour fini", target.channel, []string{subject})
}

func (m *model) notifyApproval(channel, question string) tea.Cmd {
	info := m.sessionInfo(channel)
	subject := joinNonEmpty(m.sessionName(channel), info.Vendor, strings.Join(info.Gateways, "+"))
	return m.announce(kindApproval, m.st.glyphs.attention, "Approbation en attente", channel, []string{subject, question})
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
	m.toast = toast{seq: m.toastSeq, channel: channel, glyph: glyph, title: title, body: body, signal: deliveryLabel(m.caps, m.shouldSignal()), at: time.Now()}
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

func deliveryLabel(caps Capabilities, signalled bool) string {
	if !signalled {
		return "fenêtre active, rien envoyé"
	}
	if caps.OSC9 {
		return "OSC 9 envoyé"
	}
	return "repli BEL envoyé"
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

func (m *model) toastView() string {
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
	lines := []string{toastTop(edge, titleStyle.Render(m.toast.glyph+" "+m.toast.title), m.st.dim.Render("il y a "+elapsed(time.Since(m.toast.at))), width)}
	for _, text := range m.toast.body {
		if text != "" {
			lines = append(lines, toastRowLine(edge, m.st.text.Render(text), inner))
		}
	}
	lines = append(lines,
		toastRowLine(edge, m.st.dim.Render(m.toast.signal+" · ⌃P y aller"), inner),
		edge.Render("╰"+strings.Repeat("─", width-toastCorner)+"╯"),
	)
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], m.width, "")
	}
	return strings.Join(lines, "\n")
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
	block := m.toastView()
	if block == "" || strings.Count(block, "\n")+1 > m.vp.Height {
		return rendered
	}
	column := max(0, m.width-lipgloss.Width(firstLine(block))-toastColumn)
	return overlayAt(rendered, block, toastRow, column)
}

func firstLine(block string) string {
	head, _, _ := strings.Cut(block, "\n")
	return head
}
