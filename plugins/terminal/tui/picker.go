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
	m.resumeAll = rows
	m.resumeQuery = ""
	m.applyResumeFilter()
	m.resumeOpen = true
}

func sessionSearchText(info contracts.SessionInfo) string {
	return strings.Join(append([]string{info.Name, info.Project, info.Vendor}, info.Gateways...), " ")
}

func (m *model) applyResumeFilter() {
	m.resumeRows = fuzzyFilter(m.resumeAll, m.resumeQuery, sessionSearchText)
	m.resumeIdx = 0
}

func (m *model) typeResume(text string) {
	m.resumeQuery += text
	m.applyResumeFilter()
}

func (m *model) eraseResume() {
	runes := []rune(m.resumeQuery)
	if len(runes) == 0 {
		return
	}
	m.resumeQuery = string(runes[:len(runes)-1])
	m.applyResumeFilter()
}

func (m *model) resumePreview(width int) string {
	if m.tm == nil || m.resumeIdx < 0 || m.resumeIdx >= len(m.resumeRows) {
		return ""
	}
	lines := tailLines(m.tm.Scrollback(m.resumeRows[m.resumeIdx].Name), resumePreviewLines)
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		text := strings.ReplaceAll(line.Text, "\n", " ")
		rendered = append(rendered, m.st.dim.Render(truncate("  "+text, width)))
	}
	return strings.Join(rendered, "\n")
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
	return m.openChannel(m.resumeRows[m.resumeIdx])
}

func (m *model) openChannel(session contracts.SessionInfo) tea.Cmd {
	if m.tm != nil {
		m.tm.Seen(session.Name)
	}
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
	if m.resumeQuery != "" {
		footer = "filtre : " + m.resumeQuery + " · " + footer
	}
	list := floatingList(rows, m.resumeIdx-start, footer, m.overlayWidth(), m.st)
	if preview := m.resumePreview(m.innerWidth()); preview != "" {
		list += "\n" + preview
	}
	return list
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
			mark = glyphUnread + " "
		}
		rows = append(rows, overlayRow{mark: mark, label: row.Name, detail: row.Project})
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
	count := len(m.choice.Options)
	if m.choiceIdx >= count {
		m.choiceIdx = count - 1
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
		channel := m.choice.Channel
		if channel == "" {
			channel = m.active
		}
		m.tm.Submit(channel, m.choice.Options[m.choiceIdx].Value, nil)
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
	var builder strings.Builder
	builder.WriteString(m.st.accent.Render(glyphTool + " " + m.choice.Question))
	for index, o := range m.choice.Options {
		label := fmt.Sprintf("%d. %s", index+1, o.Label)
		if index == len(m.choice.Options)-1 {
			label += "  (esc)"
		}
		if index == m.choiceIdx {
			builder.WriteString("\n" + m.st.accent.Render(glyphCursor+" "+label))
		} else {
			builder.WriteString("\n" + m.st.dim.Render("  "+label))
		}
	}
	return builder.String()
}
