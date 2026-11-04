package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const shellPrefix = "!"

type shellResultMsg struct {
	channel string
	command string
	out     string
	err     error
}

func shellCommand(text string) (string, bool) {
	if !strings.HasPrefix(text, shellPrefix) {
		return "", false
	}
	command := strings.TrimSpace(strings.TrimPrefix(text, shellPrefix))
	return command, command != ""
}

func (m *model) runShell(command string) tea.Cmd {
	tb := m.activeTab()
	name := m.sessionName(m.active)
	if tb == nil || name == "" {
		m.flash = "!commande : aucune session ouverte"
		return nil
	}
	tb.shellRunning = true
	channel := m.active
	backend := m.tm
	return func() tea.Msg {
		out, err := backend.Dispatch([]string{"session", "shell", "--name", name, "--cmd", command})
		return shellResultMsg{channel: channel, command: command, out: out, err: err}
	}
}

func (m *model) handleShellResult(msg shellResultMsg) {
	tb := m.tabs[msg.channel]
	if tb == nil {
		return
	}
	tb.shellRunning = false
	if msg.err != nil {
		tb.appendEntry(entry{role: roleError, text: msg.err.Error()})
		m.syncViewport()
		return
	}
	tb.appendEntry(entry{role: roleNotice, text: "$ " + msg.command + "\n" + msg.out})
	tb.shellContext += "$ " + msg.command + "\n" + msg.out + "\n"
	m.syncViewport()
}

func (tb *tab) withShellContext(text string) string {
	if tb.shellContext == "" {
		return text
	}
	context := "Sortie des commandes lancées par l'opérateur avec !, dans cette session :\n" + tb.shellContext
	tb.shellContext = ""
	return context + "\n" + text
}
