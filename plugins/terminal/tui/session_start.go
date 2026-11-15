package tui

import tea "github.com/charmbracelet/bubbletea"

type sessionStartedMsg struct {
	name string
	err  error
}

func (m *model) startSessionWith(text string, atts []Attachment) tea.Cmd {
	m.starting = true
	m.openText, m.openAtts = text, atts
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
		m.pending = append(m.openAtts, m.pending...)
		m.openText, m.openAtts = "", nil
		m.applySize()
		m.resizeComposer()
		m.flash = m.tr.t("tui.session.start_failed", map[string]any{tplArgReason: localizedReason(m.tr, msg.err)})
		return nil
	}
	m.flash = ""
	m.openSession = msg.name
	m.tabSig = ""
	return m.syncTabs()
}

func (m *model) giveBackFailedStart() {
	typed := m.input.Value()
	switch {
	case typed == "":
		m.input.SetValue(m.openText)
	case m.openText == "":
	default:
		m.input.SetValue(m.openText + "\n" + typed)
	}
}
