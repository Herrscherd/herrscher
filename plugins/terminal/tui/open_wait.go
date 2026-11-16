package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const openWaitLimit = 5 * time.Second

type openWait struct {
	session   string
	queued    bool
	text      string
	atts      []Attachment
	deadline  time.Time
	userMoved bool
}

func (m *model) now() time.Time {
	if m.clock == nil {
		return time.Now()
	}
	return m.clock()
}

func (m *model) beginOpen(session, text string) {
	m.open = openWait{session: session, text: text, queued: text != ""}
}

func (m *model) awaitingOpen() bool {
	return m.open.session != "" && !m.open.userMoved
}

func (m *model) noteUserMoved() {
	if m.open.session != "" {
		m.open.userMoved = true
	}
}

func (m *model) stagedAttachments() []Attachment {
	staged := make([]Attachment, 0, len(m.open.atts)+len(m.pending))
	staged = append(staged, m.open.atts...)
	return append(staged, m.pending...)
}

func (m *model) removeLastPending() bool {
	switch {
	case len(m.pending) > 0:
		m.pending = m.pending[:len(m.pending)-1]
	case len(m.open.atts) > 0:
		m.open.atts = m.open.atts[:len(m.open.atts)-1]
	default:
		return false
	}
	return true
}

func (m *model) queueForOpen(text string) {
	if text == "" && len(m.stagedAttachments()) == 0 {
		if m.open.queued {
			m.open.queued, m.open.text = false, ""
			m.flash = m.tr.t("tui.session.open_cancelled", map[string]any{tplArgName: m.open.session})
		}
		return
	}
	m.open.queued, m.open.text = true, text
	m.flash = m.tr.t("tui.session.open_pending", map[string]any{tplArgName: m.open.session})
}

func (m *model) releaseOpen() {
	name := m.open.session
	m.pending = append(m.open.atts, m.pending...)
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
	atts := m.stagedAttachments()
	m.clearOpen()
	if !open.userMoved {
		m.activate(channel)
	}
	if open.text != "" && strings.TrimSpace(m.input.Value()) == open.text {
		m.input.Reset()
		m.resizeComposer()
	}
	if !open.queued {
		return nil
	}
	m.pending = nil
	m.applySize()
	if open.text == "" && len(atts) == 0 {
		return nil
	}
	return m.submitTo(channel, open.text, atts)
}
