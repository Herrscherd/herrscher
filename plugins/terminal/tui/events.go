package tui

import (
	"log/slog"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func (m *model) route(routed RoutedEvent) tea.Cmd {
	if routed.Event.T == eventwire.EventClosed {
		m.removeTab(routed.Conv.ID)
		return nil
	}
	tab := m.ensureTab(routed.Conv.ID)
	if tab.awaitingHistory() {
		tab.pendingEvents = append(tab.pendingEvents, routed)
		return m.drainHistory()
	}
	if tab.calls.Replays(routed.Event) {
		return nil
	}
	before := len(tab.entries)
	m.settleCalls(tab, routed.Event)
	m.renderInto(tab, routed.Event)
	flushCmd := m.flushQueue(tab, routed.Event.T)
	isReply := routed.Event.T == eventwire.EventReply
	isTurnDone := isReply && routed.Event.Done
	if tab.channel != m.active {
		if len(tab.entries) != before {
			tab.unread = true
			tab.unreadCount++
		}
		if isTurnDone {
			tab.finished = true
		}
	}
	var gitCmd tea.Cmd
	if tab.channel == m.active {
		m.syncViewport()
		if routed.Event.T == eventwire.EventStatus {
			gitCmd = m.gitAfterTool(routed.Event.Text)
		}
	}
	var notice tea.Cmd
	if isTurnDone {
		notice = m.notifyTurnEnd(tab)
	}
	var images tea.Cmd
	if isReply && len(tab.entries) > 0 {
		images = m.replyImages(tab)
	}
	return tea.Batch(flushCmd, gitCmd, notice, images)
}

func (m *model) replyImages(tab *tab) tea.Cmd {
	last := lastAgentIndex(tab.entries)
	if last < 0 {
		return nil
	}
	if tab.channel == m.active && tab.modeID() == modePlan && m.planMenu == nil && tab.entries[last].text != "" {
		m.openPlanMenu(tab.channel)
	}
	return m.fetchEntryImages(tab, last)
}

func (m *model) renderInto(target *tab, event contracts.Event) {
	if event.T != eventwire.EventAbandoned {
		target.disconnected = false
	}
	previous := target.tokens
	if event.Tokens > 0 && !isCompactionEvent(event.T) {
		target.tokens = event.Tokens
	}
	target.measureContext(event)
	switch event.T {
	case eventwire.EventApproval:
		m.rememberApprovalDeadline(event, time.Now())
	case eventwire.EventApprovalRisk:
		m.rememberApprovalRisk(event, time.Now())
	case eventwire.EventApprovalSubject:
		m.rememberApprovalSubject(event, time.Now())
	case eventwire.EventMenu:
		if m.openApproval(target, event) {
			return
		}
		m.renderInto(target, contracts.Event{T: eventwire.EventStatus, Text: event.Text})
	case eventwire.EventChunk:
		target.busy = true
		if target.appendChunk(event.Text) {
			target.streamed = true
		}
	case eventwire.EventThinking:
		target.busy = true
		target.appendThinking(event.Text, target.tokens, previous)
	case eventwire.EventStatus:
		target.busy = true
		target.endStream()
		target.noteToolFile(event.Text)
		text, _ := hostError(event.Text)
		target.appendEntry(entry{role: statusRole(event.Text), text: text})
	case eventwire.EventReply:
		target.appendReply(m.tr, event)
	case eventwire.EventTodos:
		target.busy = true
		target.todos = event.Todos
	case eventwire.EventSubagent:
		target.busy = true
		target.applySubagent(event.Subagent)
	case eventwire.EventReset:
		target.streamed = false
		target.ctxMeasured = false
		target.discardStreamed()
		m.tsCache.valid = false
		target.appendEntry(entry{role: roleNotice, text: m.tr.t("tui.render.turn_reset", nil)})
	case eventwire.EventNotice:
		target.appendEntry(entry{role: roleNotice, text: event.Text, markdown: true})
	case eventwire.EventBackendNotice:
		m.appendBackendNotice(target, event)
	case eventwire.EventCompactionStarted:
		target.startCompaction(event)
	case eventwire.EventCompactionDone:
		target.completeCompaction(m.tr, event)
	case eventwire.EventCompactionFailed:
		target.failCompaction(m.tr, event)
	case eventwire.EventAbandoned:
		target.busy = false
		target.interruptRequested = false
		target.streamed = false
		target.streamedIDs = nil
		target.ctxMeasured = false
		target.disconnected = true
		target.endStream()
		target.appendEntry(entry{role: roleNotice, text: m.tr.t("tui.render.turn_abandoned", nil)})
	}
}

func (m *model) appendBackendNotice(target *tab, event contracts.Event) {
	code, err := strconv.ParseUint(event.Value, 10, 16)
	if err != nil {
		slog.Warn("backend notice not shown, its code is not a number", "value", event.Value, "err", err)
		return
	}
	text, err := contracts.NewRenderer().Render(m.tr.lang, contracts.Notice{Code: contracts.Code(code)})
	if err != nil {
		slog.Warn("backend notice not shown, it does not render", "code", code, "err", err)
		return
	}
	target.appendEntry(entry{role: roleNotice, text: text, markdown: true})
}
