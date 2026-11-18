package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func isCommandLine(text string) bool {
	_, isShell := shellCommand(text)
	return isShell || strings.HasPrefix(text, "/")
}

func (m *model) continueLine() bool {
	val := m.input.Value()
	if !strings.HasSuffix(val, `\`) || strings.HasSuffix(val, `\\`) {
		return false
	}
	m.input.SetValue(strings.TrimSuffix(val, `\`) + "\n")
	m.resizeComposer()
	return true
}

func (m *model) handleEnter() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if m.awaitingOpen() && !isCommandLine(text) {
		m.queueForOpen(text)
		return nil
	}
	if text == "" && len(m.pending) == 0 {
		return nil
	}
	if m.starting {
		m.flash = m.tr.t("tui.session.starting", nil)
		return nil
	}
	if m.tabs[m.active] == nil && !isCommandLine(text) {
		return m.startFromComposer(text)
	}
	if _, isShell := shellCommand(text); isShell && m.sessionName(m.active) == "" {
		m.flash = m.tr.t("tui.shell.no_session", nil)
		return nil
	}
	m.input.Reset()
	m.resizeComposer()
	if command, ok := shellCommand(text); ok {
		return m.runShell(command)
	}
	if strings.HasPrefix(text, "/") {
		return m.runSlash(text)
	}
	atts := m.pending
	m.pending = nil
	m.applySize()
	m.recordHistory(text)
	return m.submitTo(m.active, text, atts)
}

func (m *model) submitTo(channel, text string, atts []Attachment) tea.Cmd {
	tab := m.tabs[channel]
	if tab == nil {
		return nil
	}
	wasBusy := tab.busy
	tab.endStream()
	item := entry{role: roleYou, text: text, attachments: atts}
	item.preview = previewEscapes(m.logger(), atts, m.caps)
	tab.appendEntry(item)
	tab.busy = true
	tab.compaction.ownsBusy = false
	tab.streamed = false
	tab.tokens = 0
	tab.startedAt = time.Now()
	m.syncViewport()
	turn := queuedTurn{text: text, atts: atts}
	switch {
	case tab.snapshotting:
		tab.queued = append(tab.queued, turn)
		return nil
	case wasBusy:
		m.tm.Submit(channel, tab.withShellContext(text), atts)
		return nil
	}
	tab.snapshotting = true
	tab.queued = append(tab.queued, turn)
	return m.turnStartCmd(channel)
}

func (m *model) pasteImage() bool {
	if m.clip == nil {
		return false
	}
	mime, ok := m.clip.ImageType(m.runContext())
	if !ok {
		return false
	}
	data, err := m.clip.ReadImage(m.runContext(), mime)
	if err != nil {
		m.flash = m.tr.t("tui.status.clipboard_failed", map[string]any{tplArgReason: localizedReason(m.tr, err)})
		return true
	}
	att, err := saveClipboardImage(m.tr.catalog, data, mime, m.attachSeq)
	if err != nil {
		m.flash = localizedReason(m.tr, err)
		return true
	}
	m.attachSeq++
	m.pending = append(m.pending, att)
	m.applySize()
	m.syncViewport()
	return true
}

func (m *model) stageAttachment(path string) {
	att, err := attachLocalFile(m.tr.catalog, path, m.attachSeq)
	if err != nil {
		m.flash = localizedReason(m.tr, err)
		return
	}
	m.attachSeq++
	m.pending = append(m.pending, att)
	m.applySize()
	m.syncViewport()
}

func (m *model) recordHistory(text string) {
	if text == "" {
		return
	}
	if n := len(m.history); n == 0 || m.history[n-1] != text {
		m.history = append(m.history, text)
	}
	m.histIdx = len(m.history)
}

func (m *model) recallHistory(delta int) bool {
	if len(m.history) == 0 {
		return false
	}
	idx := m.histIdx + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.history) {
		m.histIdx = len(m.history)
		m.input.SetValue("")
		return true
	}
	m.histIdx = idx
	m.input.SetValue(m.history[idx])
	m.input.CursorEnd()
	return true
}
