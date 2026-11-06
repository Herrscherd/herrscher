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
	channelTab := m.activeTab()
	name := m.sessionName(m.active)
	if channelTab == nil || name == "" {
		m.flash = m.tr.t("tui.shell.no_session", nil)
		return nil
	}
	channelTab.shellRunning = true
	channel := m.active
	backend := m.tm
	return func() tea.Msg {
		out, err := backend.Dispatch([]string{sessionVerb, "shell", argName, name, "--cmd", command})
		return shellResultMsg{channel: channel, command: command, out: out, err: err}
	}
}

func (m *model) handleShellResult(msg shellResultMsg) {
	channelTab := m.tabs[msg.channel]
	if channelTab == nil {
		return
	}
	channelTab.shellRunning = false
	if msg.err != nil {
		channelTab.appendEntry(entry{role: roleError, text: msg.err.Error()})

		m.syncViewport()
		return
	}
	channelTab.appendEntry(entry{role: roleNotice, text: "$ " + msg.command + "\n" + msg.out})
	channelTab.shellContext += "$ " + msg.command + "\n" + msg.out + "\n"

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
