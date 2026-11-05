package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func pressCtrlD(t *testing.T, m *model) (tea.Cmd, bool) {
	t.Helper()
	run, ok := m.tables().chat[actExit]
	if !ok {
		t.Fatal("chat:exit must have a handler in the chat table")
	}
	return run(m, tea.KeyMsg{Type: tea.KeyCtrlD})
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestCtrlDQuitsOnAnIdleEmptyComposer(t *testing.T) {
	m := modeSession(&fakeBackend{sessions: namedSession()})
	cmd, handled := pressCtrlD(t, m)
	if !handled || !isQuit(cmd) {
		t.Fatal("ctrl+d with an empty composer and no busy session must quit")
	}
}

func TestCtrlDLeavesTheComposerAlone(t *testing.T) {
	m := modeSession(&fakeBackend{sessions: namedSession()})
	m.input.SetValue("brouillon")
	if _, handled := pressCtrlD(t, m); handled {
		t.Fatal("ctrl+d with text in the composer must not quit")
	}
}

func TestCtrlDInterruptsABusyActiveTurnBeforeQuitting(t *testing.T) {
	m := modeSession(&fakeBackend{sessions: namedSession()})
	m.tabs["a"].busy = true
	cmd, _ := pressCtrlD(t, m)
	if isQuit(cmd) || !m.tabs["a"].interruptRequested {
		t.Fatal("ctrl+d on a busy turn must interrupt it first, not quit")
	}
	if cmd, _ := pressCtrlD(t, m); !isQuit(cmd) {
		t.Fatal("a second ctrl+d on the interrupted turn must quit")
	}
}

func TestCtrlDAsksBeforeStoppingSessionsWorkingInOtherTabs(t *testing.T) {
	m := modeSession(&fakeBackend{sessions: namedSession()})
	m.ensureTab("b").busy = true
	cmd, handled := pressCtrlD(t, m)
	if !handled || isQuit(cmd) {
		t.Fatal("ctrl+d with another session busy must warn first, not quit")
	}
	if m.flash == "" {
		t.Fatal("the warning must name the sessions that will stop")
	}
	if cmd, _ := pressCtrlD(t, m); !isQuit(cmd) {
		t.Fatal("a second ctrl+d inside the window must quit")
	}
}
