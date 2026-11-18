//go:build e2e && linux

package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

const resumeRefusedScript = `session tok-refused-7
text premier, réponse
---
error No conversation found with session ID: tok-refused-7
---
text reprise fraîche
`

func TestE2EAttachedWindowShowsTheResumeLostNotice(t *testing.T) {
	setup := newTUISetup(t, resumeRefusedScript)
	first, stopFirst := setup.serve(t)
	window, stopWindow := attachTUIWindow(t, first)
	window.Type("premier tour")
	window.Send(tea.KeyMsg{Type: tea.KeyEnter})
	awaitTUIScreen(t, window, "premier, réponse")
	window.Send(tea.KeyMsg{Type: tea.KeyCtrlD})
	window.WaitFinished(t, teatest.WithFinalTimeout(tuiScreenWait))
	stopWindow()
	stopFirst()

	second, _ := setup.serve(t)
	reopened, _ := attachTUIWindow(t, second)
	reopened.Type("apres redemarrage")
	reopened.Send(tea.KeyMsg{Type: tea.KeyEnter})
	awaitTUIScreen(t, reopened, "the earlier conversation could not be resumed", "reprise fraîche")
}
