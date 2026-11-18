package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type dispatchResultMsg struct {
	origin string
	out    string
	err    error
}

type eventMsg RoutedEvent

type tickMsg struct{}

type sessionsMsg struct {
	seq   uint64
	infos []contracts.SessionInfo
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

type spinMsg struct{}

const fastTick = 60 * time.Millisecond

func spinTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return spinMsg{} })
}
