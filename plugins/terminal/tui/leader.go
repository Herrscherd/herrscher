package tui

import tea "github.com/charmbracelet/bubbletea"

var leaderKeys = map[string]func(*model) tea.Cmd{
	"ctrl+e": func(m *model) tea.Cmd { return m.openEditor() },
	"enter": func(m *model) tea.Cmd {
		m.sendQueueNow()
		return nil
	},
}

func (m *model) runLeader(key string) (tea.Cmd, bool) {
	action, ok := leaderKeys[key]
	if !ok {
		return nil, false
	}
	return action(m), true
}
