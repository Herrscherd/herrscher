package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	sessionVerb = "session"
	verbRename  = "rename"
	verbHandoff = "handoff"
	stateDone   = "done"
	stateActive = "active"

	stateFailed = "failed"
	stateError  = "error"
)

func (m *model) runSlash(text string) tea.Cmd {
	args := strings.Fields(strings.TrimPrefix(text, "/"))
	if len(args) == 0 {
		return nil
	}
	if cmd, handled := m.runLocalSlash(text, args); handled {
		return cmd
	}
	return m.dispatchCmd(m.active, args)
}

func (m *model) runLocalSlash(text string, args []string) (tea.Cmd, bool) {
	switch args[0] {
	case "resume":
		m.openResume()
	case "agents":
		return m.openBoard(), true
	case "clear":
		m.clearActive()
	case "help":
		m.toggleHelp()
	case cmdTheme:
		m.openTheme()
	case cmdKeybindings:
		return m.openKeybindings(), true
	case sessionVerb:
		return m.sessionSlash(args)
	case "context":
		m.openContext()
	case "health":
		return m.openHealth(), true
	case "compact":
		return m.compactCmd(), true
	case cmdAccount:
		return m.accountCmd(args[1:]), true
	case "notify":
		return m.cycleNotify(), true
	case "review":
		return m.openDiff(true), true
	case "usage":
		return m.openUsage(), true
	case "capabilities":
		m.openDiagnostics()
	case cmdSkills:
		m.openSkills()
	case "plugins":
		return m.openPlugins(), true
	case "attach":
		m.stageAttachment(strings.TrimSpace(strings.TrimPrefix(text, "/attach")))
	case verbRename:
		return m.renameCmd(args[1:]), true
	case verbHandoff:
		return m.handoffCmd(args[1:]), true
	case "copy":
		m.copyCmd(args[1:])
	case "mouse":
		return m.toggleMouse(), true
	case "statusline":
		return m.openStatusline(), true
	case "mode":
		return m.modeSlash(strings.TrimPrefix(text, "/mode")), true
	case cmdPlan:
		return m.planSlash(strings.TrimPrefix(text, "/plan")), true
	case cmdQueue:
		m.showQueue()
	default:
		return nil, false
	}
	return nil, true
}

const minSessionArgs = 2

func (m *model) sessionSlash(args []string) (tea.Cmd, bool) {
	if len(args) < minSessionArgs {
		return nil, false
	}
	switch args[1] {
	case "switch":
		m.openSwitch()
		return nil, true
	case "close":
		return m.closeCmd(args[2:]), true
	}
	return nil, false
}

func (m *model) accountCmd(args []string) tea.Cmd {
	if len(args) == 0 {
		return m.dispatchCmd(m.active, []string{"session", "accounts", m.active})
	}
	return m.dispatchCmd(m.active, setAccountArgv(m.active, args))
}

const freshWord = "fresh"

func setAccountArgv(session string, args []string) []string {
	argv := []string{"session", "set-account", session, args[0]}
	if len(args) > 1 && args[1] == freshWord {
		argv = append(argv, freshWord)
	}
	return argv
}

func (m *model) dispatchCmd(origin string, args []string) tea.Cmd {
	backend := m.tm
	return func() tea.Msg {
		out, err := backend.Dispatch(args)
		return dispatchResultMsg{origin: origin, out: out, err: err}
	}
}

func (m *model) clearActive() {
	tab := m.tabs[m.active]
	if tab == nil {
		return
	}
	tab.entries = nil
	tab.streamed = false
	m.tsCache.valid = false
	m.syncViewport()
}

func (m *model) toggleHelp() {
	m.showHelp = !m.showHelp
	m.applySize()
	m.syncViewport()
}

func (m *model) sessionName(channel string) string {
	return m.infoForChannel(channel).Name
}

func (m *model) activeSessionName() string {
	tab := m.tabs[m.active]
	if tab == nil {
		return ""
	}
	if name := m.sessionName(tab.channel); name != "" {
		return name
	}
	return tab.label
}

func (m *model) confirmClose() tea.Cmd {
	name := m.activeSessionName()
	if name == "" {
		return nil
	}
	return m.closeSession(name, false)
}

func (m *model) closeSession(name string, force bool) tea.Cmd {
	backend, manager := m.active, m.tm
	return func() tea.Msg {
		out, err := manager.Close(name, force)
		return dispatchResultMsg{origin: backend, out: out, err: err}
	}
}

func (m *model) closeCmd(rest []string) tea.Cmd {
	name, force := closeTarget(rest)
	if name == "" {
		name = m.activeSessionName()
	}
	if name == "" {
		m.flash = m.tr.t("tui.session.close_none", nil)
		return nil
	}
	return m.closeSession(name, force)
}

func (m *model) renameCmd(rest []string) tea.Cmd {
	target := renameTarget(rest)
	if target == "" {
		m.flash = m.tr.t("tui.session.rename_usage", nil)
		return nil
	}
	name := m.activeSessionName()
	if name == "" {
		m.flash = m.tr.t("tui.session.rename_none", nil)
		return nil
	}
	return m.dispatchCmd(m.active, []string{sessionVerb, verbRename, argName, name, argTo, target})
}

func (m *model) handoffCmd(rest []string) tea.Cmd {
	name := m.activeSessionName()
	if name == "" {
		m.flash = m.tr.t("tui.session.handoff_none", nil)
		return nil
	}
	args := []string{sessionVerb, verbHandoff, argName, name}
	if target := strings.Join(rest, " "); target != "" {
		args = append(args, argTo, target)
	}
	return m.dispatchCmd(m.active, args)
}

func renameTarget(rest []string) string {
	var words []string
	for index := 0; index < len(rest); index++ {
		tok := rest[index]
		switch {
		case tok == argTo:
			if index+1 < len(rest) && !strings.HasPrefix(rest[index+1], "--") {
				words = append(words, rest[index+1])
				index++
			}
		case strings.HasPrefix(tok, "--to="):
			words = append(words, strings.TrimPrefix(tok, "--to="))
		case !strings.HasPrefix(tok, "--"):
			words = append(words, tok)
		}
	}
	return strings.Join(words, " ")
}

func closeTarget(rest []string) (string, bool) {
	var name string
	var force bool
	for index := 0; index < len(rest); index++ {
		tok := rest[index]
		switch {
		case tok == "--force":
			force = true
		case tok == argName:
			if index+1 < len(rest) && !strings.HasPrefix(rest[index+1], "--") {
				name = rest[index+1]
				index++
			}
		case strings.HasPrefix(tok, "--name="):
			name = strings.TrimPrefix(tok, "--name=")
		case !strings.HasPrefix(tok, "--") && name == "":
			name = tok
		}
	}
	return name, force
}
