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
	toastWidth     = 56
	toastMinimum   = 24
	toastColumn    = 1
	toastRow       = 1
	toastFrameCols = 2
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
	detail  string
	signal  string
}

type toastExpiredMsg struct {
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
	detail := joinNonEmpty(name, time.Since(target.startedAt).Round(time.Second).String())
	return m.announce(kindTurnEnd, m.st.glyphs.success, "Tour fini", target.channel, detail)
}

func (m *model) notifyApproval(channel, question string) tea.Cmd {
	detail := joinNonEmpty(m.sessionName(channel), question)
	return m.announce(kindApproval, m.st.glyphs.attention, "Approbation en attente", channel, detail)
}

func (m *model) jumpToToast() {
	channel := m.toast.channel
	m.toast = toast{}
	m.activate(channel)
}

func (m *model) announce(kind notifyKind, glyph, title, channel, detail string) tea.Cmd {
	if !m.notifyMode.wants(kind) {
		return nil
	}
	m.toastSeq++
	m.toast = toast{seq: m.toastSeq, channel: channel, glyph: glyph, title: title, detail: detail, signal: deliveryLabel(m.caps, m.shouldSignal())}
	seq := m.toastSeq
	expire := tea.Tick(toastTTL, func(time.Time) tea.Msg { return toastExpiredMsg{seq: seq} })
	return tea.Batch(m.signalCmd(title+": "+detail), expire)
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

func (m *model) handleFocus(focused bool) {
	m.focus = focusState{focused: focused, known: true}
}

func (m *model) toastView() string {
	if m.toast.title == "" {
		return ""
	}
	inner := min(toastWidth, m.width-toastFrameCols) - toastFrameCols
	if inner < toastMinimum {
		inner = toastMinimum
	}
	titleStyle := m.st.green
	if m.toast.glyph == m.st.glyphs.attention {
		titleStyle = m.st.warn
	}
	lines := []string{
		titleStyle.Render(m.toast.glyph + " " + m.toast.title),
		m.st.text.Render(m.toast.detail),
		m.st.dim.Render(m.toast.signal + " · ctrl+p y aller"),
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], inner, "…")
	}
	return m.st.dim.Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(strings.Join(lines, "\n"))
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
	if block == "" {
		return rendered
	}
	column := max(0, m.width-lipgloss.Width(firstLine(block))-toastColumn)
	return overlayAt(rendered, block, toastRow, column)
}

func firstLine(block string) string {
	head, _, _ := strings.Cut(block, "\n")
	return head
}
