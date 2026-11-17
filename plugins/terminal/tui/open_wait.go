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
	deadline time.Time
}

func (m *model) now() time.Time {
	if m.clock == nil {
		return time.Now()
	}
	return m.clock()
}

func (m *model) beginOpen(session, text string) {
	m.heldSession = ""
	m.open = openWait{session: session, queued: text != ""}
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
		m.cancelOpenWait()
		return
	}
	m.open.queued = true
	m.flash = m.tr.t("tui.session.open_pending", map[string]any{tplArgName: m.open.session})
}

func (m *model) cancelOpenWait() {
	if !m.awaitingOpen() {
		return
	}
	name := m.open.session
	if m.active == "" {
		m.heldSession = name
	}
	m.clearOpen()
	m.flash = m.tr.t("tui.session.open_cancelled", map[string]any{tplArgName: name})
}

func (m *model) releaseHeld(name string) {
	if m.heldSession == name {
		m.heldSession = ""
	}
}

func (m *model) abandonOpen() {
	if !m.awaitingOpen() {
		return
	}
	name := m.open.session
	queued := m.open.queued
	m.clearOpen()
	if queued {
		m.flash = m.tr.t("tui.session.open_cancelled", map[string]any{tplArgName: name})
	}
}

func (m *model) endWaitAfterRemoval(successor string) {
	if !m.awaitingOpen() {
		return
	}
	if successor == "" {
		m.heldSession = m.open.session
	}
	queued := m.open.queued
	m.clearOpen()
	if queued {
		m.flash = ""
	}
}

func (m *model) releaseOpen() {
	name := m.open.session
	m.clearOpen()
	m.flash = m.tr.t("tui.session.open_missing", map[string]any{tplArgName: name})
}

func (m *model) noteTabChange(channel string) {
	if channel == "" {
		return
	}
	m.heldSession = ""
	m.abandonOpen()
}

func (m *model) clearOpen() {
	m.open = openWait{}
}

func (m *model) mayTakeFocus(name string) bool {
	return m.heldSession == "" || name != m.heldSession
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
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		m.flash = m.tr.t("tui.session.open_cancelled", map[string]any{tplArgName: open.session})
		return nil
	}
	if isCommandLine(text) {
		m.flash = m.tr.t("tui.session.open_command_kept", map[string]any{tplArgName: open.session})
		return nil
	}
	atts := m.pending
	m.pending = nil
	m.input.Reset()
	m.resizeComposer()
	m.applySize()
	return m.submitTo(channel, text, atts)
}
