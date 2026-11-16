package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const openWaitLimit = 5 * time.Second

type openWait struct {
	session  string
	queued   bool
	text     string
	deadline time.Time
}

func (m *model) now() time.Time {
	if m.clock == nil {
		return time.Now()
	}
	return m.clock()
}

func (m *model) beginOpen(session, text string) {
	m.open = openWait{session: session, queued: text != "", text: text}
	if text != "" {
		m.input.SetValue(text)
	}
}

func (m *model) awaitingOpen() bool {
	return m.open.session != ""
}

func (m *model) removeLastPending() bool {
	if len(m.pending) == 0 {
		return false
	}
	m.pending = m.pending[:len(m.pending)-1]
	return true
}

func (m *model) queueForOpen(text string) {
	if text == "" {
		m.cancelQueuedText()
		return
	}
	m.open.queued, m.open.text = true, text
	m.flash = m.tr.t("tui.session.open_pending", map[string]any{tplArgName: m.open.session})
}

func (m *model) cancelQueuedText() {
	if !m.open.queued {
		return
	}
	m.open.queued, m.open.text = false, ""
	m.flash = m.tr.t("tui.session.open_cancelled", map[string]any{tplArgName: m.open.session})
}

func (m *model) abandonOpen() {
	if m.open.session == "" {
		return
	}
	m.cancelQueuedText()
	m.clearOpen()
}

func (m *model) releaseOpen() {
	name := m.open.session
	m.clearOpen()
	m.flash = m.tr.t("tui.session.open_missing", map[string]any{tplArgName: name})
}

func (m *model) clearOpen() {
	m.open = openWait{}
}

func (m *model) focusOpen(infos []contracts.SessionInfo) tea.Cmd {
	if m.open.session == "" {
		return nil
	}
	if m.open.deadline.IsZero() {
		m.open.deadline = m.now().Add(openWaitLimit)
	}
	channel := channelOfSession(infos, m.open.session)
	if channel == "" || m.tabs[channel] == nil {
		if !m.now().Before(m.open.deadline) {
			m.releaseOpen()
		}
		return nil
	}
	return m.deliverOpen(channel)
}

func (m *model) deliverOpen(channel string) tea.Cmd {
	open := m.open
	m.clearOpen()
	m.activate(channel)
	if !open.queued {
		return nil
	}
	if strings.TrimSpace(m.input.Value()) == open.text {
		m.input.Reset()
		m.resizeComposer()
	}
	atts := m.pending
	m.pending = nil
	m.applySize()
	if open.text == "" && len(atts) == 0 {
		return nil
	}
	return m.submitTo(channel, open.text, atts)
}
