package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	leaderWindow = 1500 * time.Millisecond
	leaderKey    = "ctrl+x"
	leaderTheme  = "t"
	leaderEditor = "ctrl+e"
	leaderEnter  = "enter"
)

type leaderBinding struct {
	label string
	run   func(m *model) tea.Cmd
}

func leaderBindings() map[string]leaderBinding {
	return map[string]leaderBinding{
		leaderTheme: {label: "thème", run: func(m *model) tea.Cmd {
			m.openTheme()
			return nil
		}},
		leaderEditor: {label: "éditeur", run: func(m *model) tea.Cmd {
			return m.openEditor()
		}},
		leaderEnter: {label: "envoyer la file", run: func(m *model) tea.Cmd {
			return m.sendQueueNow()
		}},
	}
}

func (m *model) handleLeader(msg tea.KeyMsg) (tea.Cmd, bool) {
	if m.leaderArmed.IsZero() {
		if msg.String() != leaderKey {
			return nil, false
		}
		m.leaderArmed = time.Now()
		return nil, true
	}
	armedAt := m.leaderArmed
	m.leaderArmed = time.Time{}
	binding, ok := leaderBindings()[msg.String()]
	if !ok || time.Since(armedAt) > leaderWindow {
		return nil, false
	}
	return binding.run(m), true
}
