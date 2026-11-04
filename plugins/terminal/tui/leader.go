package tui

import (
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const leaderWindow = 1500 * time.Millisecond

const leaderKey = "ctrl+x"

type leaderBinding struct {
	label string
	run   func(m *model) tea.Cmd
}

func leaderBindings() map[string]leaderBinding {
	return map[string]leaderBinding{
		"t": {label: "thème", run: func(m *model) tea.Cmd {
			m.openTheme()
			return nil
		}},
		"ctrl+e": {label: "éditeur", run: func(m *model) tea.Cmd {
			return m.openEditor()
		}},
		"enter": {label: "envoyer la file", run: func(m *model) tea.Cmd {
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

func leaderHelp() string {
	bindings := leaderBindings()
	keys := make([]string, 0, len(bindings))
	for key := range bindings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, key := range keys {
		out.WriteString(" · " + leaderKey + " " + key + " " + bindings[key].label)
	}
	return out.String()
}
