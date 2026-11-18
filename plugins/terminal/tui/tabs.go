package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func (m *model) ensureTab(channel string) *tab {
	if tb, ok := m.tabs[channel]; ok {
		return tb
	}
	return m.openTab(channel, m.sessionName(channel))
}

func (m *model) ensureSessionTab(info contracts.SessionInfo) {
	m.rememberSession(info)
	m.ensureTabNamed(info.ChannelID, info.Name)
}

func (m *model) ensureTabNamed(channel, name string) *tab {
	if existing, ok := m.tabs[channel]; ok {
		if existing.history == historyNone && len(existing.entries) == 0 {
			m.queueHistory(existing, name)
		}
		return existing
	}
	return m.openTab(channel, name)
}

func (m *model) openTab(channel, name string) *tab {
	tab := &tab{channel: channel, label: channel, openedAt: time.Now()}
	m.tabs[channel] = tab
	m.order = append(m.order, channel)
	if m.active == "" && m.mayTakeFocus(name) {
		m.active = channel
		m.applySize()
	}
	m.queueHistory(tab, name)
	if m.active == channel {
		m.syncViewport()
		m.vp.GotoBottom()
	}
	return tab
}

func (m *model) removeTab(channel string) {
	m.removePane(channel)
	if _, ok := m.tabs[channel]; !ok {
		return
	}
	delete(m.tabs, channel)
	out := m.order[:0]
	for _, channelName := range m.order {
		if channelName != channel {
			out = append(out, channelName)
		}
	}
	m.order = out
	if m.active == channel {
		successor := m.successorTab()
		m.endWaitAfterRemoval(successor)
		m.active = ""
		m.activate(successor)
		m.syncViewport()
	}
}

func (m *model) successorTab() string {
	if focused := m.focusedChannel(); m.surface == surfacePanes && m.tabs[focused] != nil {
		return focused
	}
	if len(m.order) > 0 {
		return m.order[0]
	}
	return ""
}

func (m *model) syncTabs() tea.Cmd {
	m.sessionsIssued++
	m.sessionsApplied = m.sessionsIssued
	return m.applySessions(m.tm.Sessions())
}

func (m *model) applySessions(infos []contracts.SessionInfo) tea.Cmd {
	if infos == nil {
		return m.focusOpen(nil)
	}
	m.rememberSessions(infos)
	var builder strings.Builder
	for i := range infos {
		info := &infos[i]
		builder.WriteString(info.ChannelID)
		builder.WriteByte('\x1f')
		builder.WriteString(info.Name)
		builder.WriteByte('\n')
	}
	sig := builder.String()
	if sig == m.tabSig {
		return m.focusOpen(infos)
	}
	m.tabSig = sig
	live := make(map[string]bool)
	for i := range infos {
		session := &infos[i]
		live[session.ChannelID] = true
		if session.Archived {
			continue
		}
		tab := m.ensureTabNamed(session.ChannelID, session.Name)
		if session.Name != "" {
			tab.label = session.Name
		}
	}
	for _, channel := range append([]string(nil), m.order...) {
		if !live[channel] {
			m.removeTab(channel)
		}
	}
	return m.focusOpen(infos)
}

func (m *model) switchTab(delta int) {
	if len(m.order) == 0 {
		return
	}
	idx := 0
	for index, ch := range m.order {
		if ch == m.active {
			idx = index
			break
		}
	}
	idx = (idx + delta + len(m.order)) % len(m.order)
	m.activate(m.order[idx])
}

func (m *model) activate(channel string) {
	m.noteTabChange(channel)
	if m.tabs[channel] == nil {
		return
	}
	if channel == m.active {
		return
	}
	m.active = channel
	m.tabs[channel].unread = false
	m.tabs[channel].unreadCount = 0
	m.tabs[channel].finished = false
	m.syncViewport()
	m.vp.GotoBottom()
}
