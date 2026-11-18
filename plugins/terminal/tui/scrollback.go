package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type scrollbackMsg struct {
	channel string
	seq     uint64
	lines   []contracts.ScrollbackLine
	err     error
}

func (m *model) queueHistory(tab *tab, name string) {
	if m.tm == nil || name == "" {
		return
	}
	m.historySeq++
	tab.history = historyQueued
	tab.historySeq = m.historySeq
	tab.historyName = name
}

func (m *model) drainHistory() tea.Cmd {
	var cmds []tea.Cmd
	for _, channel := range m.order {
		channelTab := m.tabs[channel]
		if channelTab == nil || channelTab.history != historyQueued {
			continue
		}
		channelTab.history = historyRequested
		cmds = append(cmds, m.loadHistory(channelTab))
	}
	return tea.Batch(cmds...)
}

func (m *model) loadHistory(tb *tab) tea.Cmd {
	backend, channel, name, seq := m.tm, tb.channel, tb.historyName, tb.historySeq
	return func() tea.Msg {
		lines, err := backend.Scrollback(name)
		return scrollbackMsg{channel: channel, seq: seq, lines: lines, err: err}
	}
}

func (m *model) handleScrollback(msg scrollbackMsg) tea.Cmd {
	channelTab := m.tabs[msg.channel]
	if channelTab == nil || channelTab.history != historyRequested || channelTab.historySeq != msg.seq {
		return nil
	}
	channelTab.history = historyDone
	if msg.err != nil {
		channelTab.appendEntry(entry{role: roleError, text: m.tr.t("tui.session.history_failed", map[string]any{tplArgReason: localizedReason(m.tr, msg.err)})})
	} else {
		m.seedScrollback(channelTab, msg.lines)
	}
	pending := unseenPending(msg.lines, channelTab.pendingEvents)
	channelTab.pendingEvents = nil
	cmds := make([]tea.Cmd, 0, len(pending))
	for i := range pending {
		cmds = append(cmds, m.route(pending[i]))
	}
	if m.active == channelTab.channel {
		m.syncViewport()
		m.vp.GotoBottom()
	}
	return tea.Batch(cmds...)
}

func (m *model) seedScrollback(channelTab *tab, lines []contracts.ScrollbackLine) {
	if len(lines) == 0 {
		return
	}
	for _, ln := range lines {
		channelTab.appendEntry(entry{role: scrollbackRole(ln.Role), text: ln.Text})
	}
	channelTab.appendEntry(entry{role: roleScrollback, text: m.tr.t("tui.render.earlier", nil)})
}

func scrollbackRole(role string) string {
	if role == "user" {
		return roleYou
	}
	return roleAgent
}
