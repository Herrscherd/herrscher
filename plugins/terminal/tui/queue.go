package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	queueEndOfTurn   = "fin du tour"
	queueAfterTool   = "après le prochain outil"
	queueRemoveLabel = "✕ retirer"
	queueIndent      = "   "
	draftsKey        = "⌃S"
	queueAfterMark   = "↪ réorienter"
	queueFileMark    = "○ file"
	queueHeaderMark  = "○"
)

type queueHit struct {
	row, from, to, index int
}

type queuedInput struct {
	text  string
	steer bool
	atts  []Attachment
}

func (m *model) activeTab() *tab { return m.tabs[m.active] }

func (m *model) enqueueInput(steer bool) bool {
	tb := m.activeTab()
	text := strings.TrimSpace(m.input.Value())
	if tb == nil || (text == "" && len(m.pending) == 0) {
		return false
	}
	tb.queue = append(tb.queue, queuedInput{text: text, steer: steer, atts: m.pending})
	m.pending = nil
	m.input.Reset()
	m.resizeComposer()
	m.recordHistory(text)
	m.applySize()
	m.syncViewport()
	return true
}

func (m *model) flushQueue(tb *tab, event string) {
	if tb == nil || len(tb.queue) == 0 || tb.disconnected {
		return
	}
	index := -1
	switch {
	case !tb.busy:
		index = 0
	case event == "status":
		index = steerIndex(tb.queue)
	}
	if index < 0 {
		return
	}
	next := tb.queue[index]
	tb.queue = append(tb.queue[:index], tb.queue[index+1:]...)
	m.submitTo(tb.channel, next.text, next.atts)
}

func steerIndex(queue []queuedInput) int {
	for i, item := range queue {
		if item.steer {
			return i
		}
	}
	return -1
}

func (m *model) sendQueueNow() {
	tb := m.activeTab()
	if tb == nil || len(tb.queue) == 0 {
		return
	}
	if tb.busy {
		if name := m.sessionName(m.active); name != "" {
			m.tm.Interrupt(name)
		}
		return
	}
	m.flushQueue(tb, "")
	m.syncViewport()
}

func (m *model) stashDraft() {
	if strings.TrimSpace(m.input.Value()) == "" {
		n := len(m.drafts)
		if n == 0 {
			return
		}
		m.input.SetValue(m.drafts[n-1])
		m.input.CursorEnd()
		m.drafts = m.drafts[:n-1]
		m.resizeComposer()
		return
	}
	m.drafts = append(m.drafts, m.input.Value())
	m.input.Reset()
	m.resizeComposer()
}

func (m *model) draftsView() string {
	if len(m.drafts) == 0 {
		return ""
	}
	last := oneLine(m.drafts[len(m.drafts)-1])
	text := "○ brouillons (" + strconv.Itoa(len(m.drafts)) + ") : « " + truncate(last, m.innerWidth()/2) + " » " + draftsKey + " de côté · reprendre le dernier"
	return m.st.dim.Render(truncate(text, m.innerWidth()))
}

func (m *model) queueLines(base int) []string {
	m.queueHits = nil
	tb := m.activeTab()
	if tb == nil || len(tb.queue) == 0 {
		return nil
	}
	m.clampQueueSel()
	width := m.innerWidth()
	header := queueHeaderMark + " En file (" + strconv.Itoa(len(tb.queue)) + ")"
	if tb.busy {
		header += " · partent dans l'ordre à la fin du tour · ⌃⏎ tout envoyer maintenant"
	} else {
		header += " · prêts à partir"
	}
	lines := []string{m.st.dim.Render(truncate(header, width))}
	for i, item := range tb.queue {
		mark, when := queueFileMark, queueEndOfTurn
		if item.steer {
			mark, when = queueAfterMark, queueAfterTool
		}
		left := queueIndent + strconv.Itoa(i+1) + "  " + mark + "  " + oneLine(item.text)
		right := when + "  " + queueRemoveLabel
		row := justifyRow(left, m.st.dim.Render(right), width)
		if m.queueFocus && i == m.queueSel {
			row = m.st.overlaySel.Render(justifyRow(left, right, width))
		}
		m.queueHits = append(m.queueHits, queueHit{row: base + len(lines), from: width - lipgloss.Width(queueRemoveLabel), to: width, index: i})
		lines = append(lines, row)
	}
	return lines
}

func justifyRow(left, right string, width int) string {
	room := width - lipgloss.Width(right) - 1
	if room < 0 {
		room = 0
	}
	left = truncate(left, room)
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *model) clampQueueSel() {
	tb := m.activeTab()
	count := 0
	if tb != nil {
		count = len(tb.queue)
	}
	if count == 0 {
		m.queueFocus = false
		m.queueSel = 0
		return
	}
	if m.queueSel >= count {
		m.queueSel = count - 1
	}
	if m.queueSel < 0 {
		m.queueSel = 0
	}
}

func (m *model) focusQueueStart() bool {
	tb := m.activeTab()
	if tb == nil || len(tb.queue) == 0 || m.input.Value() != "" {
		return false
	}
	m.queueFocus = true
	m.queueSel = len(tb.queue) - 1
	return true
}

func (m *model) handleQueueFocus(msg tea.KeyMsg) (bool, tea.Cmd) {
	if !m.queueFocus {
		return false, nil
	}
	switch msg.Type {
	case tea.KeyUp:
		if m.queueSel > 0 {
			m.queueSel--
		}
	case tea.KeyDown:
		if m.queueSel < len(m.activeTab().queue)-1 {
			m.queueSel++
		} else {
			m.queueFocus = false
		}
	case tea.KeyDelete:
		m.removeQueued(m.queueSel)
	case tea.KeyEsc:
		m.queueFocus = false
	default:
		m.queueFocus = false
		return false, nil
	}
	return true, nil
}

func (m *model) removeQueued(index int) bool {
	tb := m.activeTab()
	if tb == nil || index < 0 || index >= len(tb.queue) {
		return false
	}
	tb.queue = append(tb.queue[:index], tb.queue[index+1:]...)
	m.clampQueueSel()
	m.applySize()
	m.syncViewport()
	return true
}

func (m *model) handleQueueClick(msg tea.MouseMsg) bool {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return false
	}
	for _, hit := range m.queueHits {
		if msg.Y == hit.row && msg.X >= hit.from && msg.X < hit.to {
			return m.removeQueued(hit.index)
		}
	}
	return false
}

func (m *model) showQueue() {
	tb := m.activeTab()
	if tb == nil {
		return
	}
	if len(tb.queue) == 0 {
		tb.appendEntry(entry{role: roleNotice, text: "la file est vide"})
		m.syncViewport()
		return
	}
	lines := make([]string, 0, len(tb.queue))
	for i, item := range tb.queue {
		lines = append(lines, strconv.Itoa(i+1)+". "+oneLine(item.text))
	}
	tb.appendEntry(entry{role: roleNotice, text: "file (" + strconv.Itoa(len(lines)) + ") :\n" + strings.Join(lines, "\n")})
	m.syncViewport()
}

func (m *model) enqueueWhileBusy(steer bool) bool {
	tb := m.activeTab()
	text := strings.TrimSpace(m.input.Value())
	if tb == nil || !tb.busy || strings.HasPrefix(text, "/") || strings.HasPrefix(text, "!") {
		return false
	}
	return m.enqueueInput(steer)
}
