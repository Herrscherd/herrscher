package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func (m *model) openResume() {
	rows := append([]contracts.SessionInfo(nil), m.tm.Sessions()...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].LastTs > rows[j].LastTs })
	m.resumeRows = rows
	m.resumeIdx = 0
	m.resumeOpen = true
}

func (m *model) clampResume() {
	count := len(m.resumeRows)
	if m.resumeIdx >= count {
		m.resumeIdx = count - 1
	}
	if m.resumeIdx < 0 {
		m.resumeIdx = 0
	}
}

func (m *model) moveResume(d int) { m.resumeIdx += d; m.clampResume() }

func (m *model) chooseResume() tea.Cmd {
	if m.resumeIdx < 0 || m.resumeIdx >= len(m.resumeRows) {
		return nil
	}
	session := m.resumeRows[m.resumeIdx]
	m.ensureTab(session.ChannelID)
	m.active = session.ChannelID
	if tb := m.tabs[session.ChannelID]; tb != nil {
		tb.unread = false
	}
	m.syncViewport()
	if !session.Archived {
		return nil
	}
	sessionName, channel, manager := session.Name, session.ChannelID, m.tm
	return func() tea.Msg {
		out, err := manager.Resume(sessionName)
		return dispatchResultMsg{origin: channel, out: out, err: err}
	}
}

func (m *model) resumeView() string {
	if len(m.resumeRows) == 0 {
		return m.st.dim.Render("  (aucune session)")
	}
	start, end := paletteWindow(len(m.resumeRows), m.resumeIdx, m.overlayRows())
	rows := make([]overlayRow, 0, end-start)
	for i := start; i < end; i++ {
		row := m.resumeRows[i]
		state := "live"
		if row.Archived {
			state = "archivée"
		}
		cols := []string{}
		if row.Project != "" {
			cols = append(cols, row.Project)
		}
		if row.LastTs != "" {
			cols = append(cols, row.LastTs)
		}
		if row.Vendor != "" {
			cols = append(cols, row.Vendor)
		}
		cols = append(cols, state)
		mark := "  "
		if row.Resumable {
			mark = "⟲ "
		}
		rows = append(rows, overlayRow{mark: mark, label: row.Name, detail: strings.Join(cols, " · ")})
	}
	footer := "↑↓ choisir · ⇞⇟ page · Entrée ouvrir · Échap annuler"
	if hidden := len(m.resumeRows) - (end - start); hidden > 0 {
		footer = fmt.Sprintf("+%d autres · %s", hidden, footer)
	}
	return floatingList(rows, m.resumeIdx-start, footer, m.overlayWidth(), m.st)
}

func (m *model) openSwitch() {
	rows := append([]contracts.SessionInfo(nil), m.tm.Sessions()...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].LastTs > rows[j].LastTs })
	m.switchRows = rows
	m.switchIdx = 0
	m.switchOpen = true
}

func (m *model) clampSwitch() {
	count := len(m.switchRows)
	if m.switchIdx >= count {
		m.switchIdx = count - 1
	}
	if m.switchIdx < 0 {
		m.switchIdx = 0
	}
}

func (m *model) moveSwitch(d int) { m.switchIdx += d; m.clampSwitch() }

func (m *model) chooseSwitch() {
	if m.switchIdx < 0 || m.switchIdx >= len(m.switchRows) {
		return
	}
	row := m.switchRows[m.switchIdx]
	m.ensureTab(row.ChannelID)
	m.active = row.ChannelID
	if tb := m.tabs[row.ChannelID]; tb != nil {
		tb.unread = false
	}
	m.syncViewport()
}

func (m *model) switchView() string {
	if len(m.switchRows) == 0 {
		return m.st.dim.Render("  (aucune session)")
	}
	start, end := paletteWindow(len(m.switchRows), m.switchIdx, m.overlayRows())
	rows := make([]overlayRow, 0, end-start)
	for i := start; i < end; i++ {
		row := m.switchRows[i]
		mark := "  "
		if tb := m.tabs[row.ChannelID]; tb != nil && tb.unread {
			mark = m.st.glyphs.unread + " "
		}
		rows = append(rows, overlayRow{mark: mark, label: row.Name, detail: row.Project})
	}
	footer := "↑↓ choisir · ⇞⇟ page · Entrée basculer · Échap annuler"
	if hidden := len(m.switchRows) - (end - start); hidden > 0 {
		footer = fmt.Sprintf("+%d autres · %s", hidden, footer)
	}
	return floatingList(rows, m.switchIdx-start, footer, m.overlayWidth(), m.st)
}
