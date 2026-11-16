package tui

import tea "github.com/charmbracelet/bubbletea"

type sessionStartedMsg struct {
	name string
	err  error
}

func (m *model) startSessionWith(text string, atts []Attachment) tea.Cmd {
	m.starting = true
	m.open.text, m.open.atts, m.open.queued = text, atts, true
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
		m.giveBackFailedStart()
		m.pending = append(m.open.atts, m.pending...)
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

func (m *model) giveBackFailedStart() {
	typed := m.input.Value()
	switch {
	case typed == "":
		m.input.SetValue(m.open.text)
	case m.open.text == "":
	default:
		m.input.SetValue(m.open.text + "\n" + typed)
	}
}
