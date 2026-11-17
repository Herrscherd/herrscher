package tui

import tea "github.com/charmbracelet/bubbletea"

type sessionStartedMsg struct {
	name string
	err  error
}

func (m *model) startFromComposer(text string) tea.Cmd {
	if text == "" {
		if len(m.pending) > 0 {
			m.flash = m.tr.t("tui.session.attachment_needs_text", nil)
		}
		return nil
	}
	m.recordHistory(text)
	return m.startSession()
}

func (m *model) startSession() tea.Cmd {
	m.starting = true
	m.heldSession = ""
	m.open.queued = true
	m.flash = m.tr.t("tui.session.starting", nil)
	backend := m.tm
	return func() tea.Msg {
		name, err := backend.StartSession()
		return sessionStartedMsg{name: name, err: err}
	}
}

func (m *model) handleSessionStarted(msg sessionStartedMsg) tea.Cmd {
	m.starting = false
	if msg.err != nil {
		m.clearOpen()
		m.applySize()
		m.resizeComposer()
		m.flash = m.tr.t("tui.session.start_failed", map[string]any{tplArgReason: localizedReason(m.tr, msg.err)})
		return nil
	}
	m.flash = ""
	m.open.session = msg.name
	m.tabSig = ""
	return m.syncTabs()
}
