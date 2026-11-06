package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	draftPreviewDivisor = 2
	queueIndent         = "   "
	queueHeaderMark     = "○"
	queueMarkWidth      = 14
	queueFoldRows       = 2
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
	current := m.activeTab()
	text := strings.TrimSpace(m.input.Value())
	if current == nil || (text == "" && len(m.pending) == 0) {
		return false
	}
	current.queue = append(current.queue, queuedInput{text: text, steer: steer, atts: m.pending})
	m.pending = nil
	m.input.Reset()
	m.resizeComposer()
	m.recordHistory(text)
	m.applySize()
	m.syncViewport()
	return true
}

func (m *model) flushQueue(current *tab, event string) tea.Cmd {
	if current == nil || len(current.queue) == 0 || current.disconnected {
		return nil
	}
	index := -1
	switch {
	case !current.busy:
		index = 0
	case event == eventStatus:
		index = steerIndex(current.queue)
	}
	if index < 0 {
		return nil
	}
	next := current.queue[index]
	current.queue = append(current.queue[:index], current.queue[index+1:]...)
	return m.submitTo(current.channel, next.text, next.atts)
}

func steerIndex(queue []queuedInput) int {
	for index, item := range queue {
		if item.steer {
			return index
		}
	}
	return -1
}

func (m *model) sendQueueNow() tea.Cmd {
	current := m.activeTab()
	if current == nil || len(current.queue) == 0 {
		return nil
	}
	if current.busy {
		if name := m.sessionName(m.active); name != "" {
			m.tm.Interrupt(name)
		}
		return nil
	}
	cmd := m.flushQueue(current, "")
	m.syncViewport()
	return cmd
}

func (m *model) stashDraft() {
	if strings.TrimSpace(m.input.Value()) == "" {
		count := len(m.drafts)
		if count == 0 {
			return
		}
		m.input.SetValue(m.drafts[count-1])
		m.input.CursorEnd()
		m.drafts = m.drafts[:count-1]
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
	left := m.st.dim.Render(" ○ " + m.tr.t("tui.queue.drafts", map[string]any{tplArgCount: len(m.drafts), tplArgText: cutText(last, m.innerWidth()/draftPreviewDivisor)}))
	right := m.st.accent.Render(m.hintIn(actStash, chatKeyStack()...)) + m.st.dim.Render(" "+m.tr.t("tui.queue.resume_last", nil))
	return justifyRow(left, right, m.innerWidth())
}

func (m *model) queueLines(base, rows int) []string {
	m.queueHits = nil
	current := m.activeTab()
	if current == nil || len(current.queue) == 0 || rows < 1 {
		return nil
	}
	m.clampQueueSel()
	width := m.innerWidth()
	header := queueHeaderMark + " " + m.tr.t("tui.queue.title", nil) + " (" + strconv.Itoa(len(current.queue)) + ")"
	if current.busy {
		header += footerSeparator + joinNonEmpty(m.tr.t("tui.queue.busy_note", nil), m.labelledHint(actQueueSubmit, m.tr.t("tui.queue.send_all", nil), chatKeyStack()...))
	} else {
		header += " · " + m.tr.t("tui.queue.ready", nil)
	}
	if m.queueFocus {
		header = m.queueFocusText()
	}
	lines := []string{m.st.dim.Render(foldText(header, width))}
	shown := queueShown(len(current.queue), rows)
	numberWidth := len(strconv.Itoa(len(current.queue)))
	removeLabel := m.tr.t("tui.queue.remove", nil)
	for index, item := range current.queue[:shown] {
		mark, when := m.tr.t("tui.queue.file_mark", nil), m.tr.t("tui.queue.end_of_turn", nil)
		if item.steer {
			mark, when = m.tr.t("tui.queue.redirect_mark", nil), m.tr.t("tui.queue.after_tool", nil)
		}
		left := queueIndent + padLeft(strconv.Itoa(index+1), numberWidth) + "  " + padTo(mark, queueMarkWidth) + oneLine(item.text)
		right := when + "  " + removeLabel
		row := justifyRow(left, m.st.dim.Render(right), width)
		if m.queueFocus && index == m.queueSel {
			row = m.st.overlaySel.Render(justifyRow(left, right, width))
		}
		m.queueHits = append(m.queueHits, queueHit{row: base + len(lines), from: width - lipgloss.Width(removeLabel), to: width, index: index})
		lines = append(lines, row)
	}
	if hidden := len(current.queue) - shown; hidden > 0 && rows >= queueFoldRows {
		lines = append(lines, m.st.dim.Render(queueIndent+m.tr.t("tui.queue.more", map[string]any{tplArgCount: hidden})))
	}
	return lines
}

func queueShown(total, rows int) int {
	if total <= rows-1 {
		return total
	}
	return max(0, rows-queueFoldRows)
}

func padLeft(text string, width int) string {
	return strings.Repeat(" ", max(0, width-len(text))) + text
}

func justifyRow(left, right string, width int) string {
	room := width - lipgloss.Width(right) - 1
	if room < 0 {
		room = 0
	}
	left = cutText(left, room)
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *model) clampQueueSel() {
	current := m.activeTab()
	count := 0
	if current != nil {
		count = len(current.queue)
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
	current := m.activeTab()
	if current == nil || len(current.queue) == 0 || m.input.Value() != "" {
		return false
	}
	m.queueFocus = true
	m.queueSel = len(current.queue) - 1
	return true
}

func (m *model) handleQueueFocus(msg tea.KeyMsg) bool {
	if !m.queueFocus {
		return false
	}
	chosen := m.stackAction(msg, ctxQueue)
	if run, ok := m.tables().queue[chosen]; ok {
		return run(m, msg)
	}
	m.queueFocus = false
	return false
}

func queueRuns() (map[keyAction]flagRun, error) {
	return dispatchOf(
		bind[flagRun](actQueuePrevious, stepFlag(func(app *model) {
			if app.queueSel > 0 {
				app.queueSel--
			}
		})),
		bind[flagRun](actQueueNext, stepFlag(func(app *model) {
			if app.queueSel < len(app.activeTab().queue)-1 {
				app.queueSel++
			} else {
				app.queueFocus = false
			}
		})),
		bind[flagRun](actQueueMoveUp, stepFlag(func(app *model) { app.moveQueued(app.queueSel, -1) })),
		bind[flagRun](actQueueMoveDown, stepFlag(func(app *model) { app.moveQueued(app.queueSel, 1) })),
		bind[flagRun](actQueueRemove, stepFlag(func(app *model) { app.removeQueued(app.queueSel) })),
		bind[flagRun](actQueueLeave, stepFlag(func(app *model) { app.queueFocus = false })),
	)
}

func (m *model) moveQueued(index, delta int) {
	current := m.activeTab()
	target := index + delta
	if current == nil || index < 0 || index >= len(current.queue) || target < 0 || target >= len(current.queue) {
		return
	}
	current.queue[index], current.queue[target] = current.queue[target], current.queue[index]
	m.queueSel = target
	m.syncViewport()
}

func (m *model) removeQueued(index int) bool {
	current := m.activeTab()
	if current == nil || index < 0 || index >= len(current.queue) {
		return false
	}
	current.queue = append(current.queue[:index], current.queue[index+1:]...)
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
	current := m.activeTab()
	if current == nil {
		return
	}
	if len(current.queue) == 0 {
		current.appendEntry(entry{role: roleNotice, text: m.tr.t("tui.queue.empty", nil)})
		m.syncViewport()
		return
	}
	lines := make([]string, 0, len(current.queue))
	for index, item := range current.queue {
		lines = append(lines, strconv.Itoa(index+1)+". "+oneLine(item.text))
	}

	current.appendEntry(entry{role: roleNotice, text: m.tr.t("tui.queue.list", map[string]any{tplArgCount: len(lines), "Lines": strings.Join(lines, "\n")})})
	m.syncViewport()
}

func (m *model) enqueueWhileBusy(steer bool) bool {
	current := m.activeTab()
	text := strings.TrimSpace(m.input.Value())
	if current == nil || !current.busy || strings.HasPrefix(text, "/") || strings.HasPrefix(text, "!") {
		return false
	}
	return m.enqueueInput(steer)
}

func (m *model) queueFocusText() string {
	return queueHeaderMark + " " + m.tr.t("tui.queue.title", nil) + footerSeparator + joinNonEmpty(
		m.pairHint(actQueuePrevious, actQueueNext, m.tr.t("tui.common.choose", nil), ctxQueue),
		m.pairHint(actQueueMoveUp, actQueueMoveDown, m.tr.t("tui.queue.reorder", nil), ctxQueue),
		m.labelledHint(actQueueRemove, m.tr.t("tui.queue.remove_action", nil), ctxQueue),
		m.labelledHint(actQueueLeave, m.tr.t("tui.queue.leave", nil), ctxQueue),
	)
}
