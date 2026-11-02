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
	channel, ch, tm := session.Name, session.ChannelID, m.tm
	return func() tea.Msg {
		out, err := tm.Resume(channel)
		return dispatchResultMsg{origin: ch, out: out, err: err}
	}
}

func (m *model) resumeView() string {
	if len(m.resumeRows) == 0 {
		return m.st.dim.Render("  (aucune session)")
	}
	start, end := paletteWindow(len(m.resumeRows), m.resumeIdx, m.overlayRows())
	rows := make([]overlayRow, 0, end-start)
	for i := start; i < end; i++ {
		s := m.resumeRows[i]
		state := "live"
		if s.Archived {
			state = "archivée"
		}
		cols := []string{}
		if s.Project != "" {
			cols = append(cols, s.Project)
		}
		if s.LastTs != "" {
			cols = append(cols, s.LastTs)
		}
		if s.Vendor != "" {
			cols = append(cols, s.Vendor)
		}
		cols = append(cols, state)
		mark := "  "
		if s.Resumable {
			mark = "⟲ "
		}
		rows = append(rows, overlayRow{mark: mark, label: s.Name, detail: strings.Join(cols, " · ")})
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
	n := len(m.switchRows)
	if m.switchIdx >= n {
		m.switchIdx = n - 1
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
	s := m.switchRows[m.switchIdx]
	m.ensureTab(s.ChannelID)
	m.active = s.ChannelID
	if tb := m.tabs[s.ChannelID]; tb != nil {
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
		s := m.switchRows[i]
		mark := "  "
		if tb := m.tabs[s.ChannelID]; tb != nil && tb.unread {
			mark = glyphUnread + " "
		}
		rows = append(rows, overlayRow{mark: mark, label: s.Name, detail: s.Project})
	}
	footer := "↑↓ choisir · ⇞⇟ page · Entrée basculer · Échap annuler"
	if hidden := len(m.switchRows) - (end - start); hidden > 0 {
		footer = fmt.Sprintf("+%d autres · %s", hidden, footer)
	}
	return floatingList(rows, m.switchIdx-start, footer, m.overlayWidth(), m.st)
}

// ChoiceOption is one selectable answer in a permission menu: Label is shown to
// the operator, Value is the frame value submitted to the backend when chosen.
type ChoiceOption struct {
	Label string
	Value string
}

// PendingChoice is an active allow/deny permission menu awaiting a selection.
// Question frames what is being permitted (e.g. "run Bash(rm -rf build/)");
// Channel is the session the answer is submitted back to.
type PendingChoice struct {
	Question string
	Options  []ChoiceOption
	Channel  string
}

func (m *model) openChoice(pc *PendingChoice) {
	m.choice = pc
	m.choiceIdx = 0
	m.applySize()
	m.syncViewport()
}

func (m *model) clampChoice() {
	if m.choice == nil {
		return
	}
	n := len(m.choice.Options)
	if m.choiceIdx >= n {
		m.choiceIdx = n - 1
	}
	if m.choiceIdx < 0 {
		m.choiceIdx = 0
	}
}

func (m *model) moveChoice(d int) { m.choiceIdx += d; m.clampChoice() }

func (m *model) chooseChoice() {
	if m.choice == nil {
		return
	}
	if m.choiceIdx >= 0 && m.choiceIdx < len(m.choice.Options) {
		ch := m.choice.Channel
		if ch == "" {
			ch = m.active
		}
		m.tm.Submit(ch, m.choice.Options[m.choiceIdx].Value, nil)
	}
	m.choice = nil
	m.choiceIdx = 0
	m.applySize()
	m.syncViewport()
}

func (m *model) choiceView() string {
	if m.choice == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.st.accent.Render(glyphTool + " " + m.choice.Question))
	for index, o := range m.choice.Options {
		label := fmt.Sprintf("%d. %s", index+1, o.Label)
		if index == len(m.choice.Options)-1 {
			label += "  (esc)"
		}
		if index == m.choiceIdx {
			b.WriteString("\n" + m.st.accent.Render(glyphCursor+" "+label))
		} else {
			b.WriteString("\n" + m.st.dim.Render("  "+label))
		}
	}
	return b.String()
}
