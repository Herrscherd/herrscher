package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m *model) handleSwitchKey(msg tea.KeyMsg) tea.Cmd {
	action := m.stackAction(msg, ctxSessions)
	if m.overlayNav(action, m.moveSwitch) {
		return nil
	}
	if run, ok := m.tables().switchOverlay[action]; ok {
		return run(m, msg)
	}
	return nil
}

func switchOverlayRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actSelectCancel, stepKey(func(app *model) {
			app.switchOpen = false
			app.relayout()
		})),
		bind[keyRun](actSelectAccept, cmdKey(func(app *model) tea.Cmd {
			app.chooseSwitch()
			app.switchOpen = false
			app.relayout()
			return app.ensureSpin()
		})),
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
	)
}

func (m *model) handleSearchKey(msg tea.KeyMsg) tea.Cmd {
	action := m.stackAction(msg, ctxSearch)
	if run, ok := m.tables().search[action]; ok {
		return run(m, msg)
	}
	if action != "" {
		return nil
	}
	switch msg.Type {
	case tea.KeyBackspace:
		if q := m.searchQuery; q != "" {
			m.typeSearch(q[:len(q)-1])
		}
	case tea.KeyRunes:
		if !msg.Alt {
			m.typeSearch(m.searchQuery + string(msg.Runes))
		}
	case tea.KeySpace:
		m.typeSearch(m.searchQuery + " ")
	}
	return nil
}

func searchRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
		bind[keyRun](actSearchClose, stepKey((*model).closeSearch)),
		bind[keyRun](actSearchNext, stepKey(func(app *model) { app.searchStep(1) })),
		bind[keyRun](actSearchPrevious, stepKey(func(app *model) { app.searchStep(-1) })),
	)
}

func (m *model) handleDiagKey(msg tea.KeyMsg) tea.Cmd {
	chosen := m.stackAction(msg, ctxDiagnostics)
	if run, ok := m.tables().diag[chosen]; ok {
		return run(m, msg)
	}
	return nil
}

func diagRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
		bind[keyRun](actDiagnosticsClose, stepKey(func(app *model) {
			app.diagOpen = false
			app.relayout()
		})),
	)
}

func (m *model) handlePluginsOverlayKey(msg tea.KeyMsg) tea.Cmd {
	return m.handlePluginsKey(msg)
}

func (m *model) handleSkillsKey(msg tea.KeyMsg) tea.Cmd {
	action := m.stackAction(msg, ctxSkills)
	if m.overlayNav(action, m.moveSkills) {
		return nil
	}
	if run, ok := m.tables().skills[action]; ok {
		return run(m, msg)
	}
	return nil
}

func skillsRuns() (map[keyAction]keyRun, error) {
	closeSkills := stepKey(func(app *model) {
		app.skillsOpen = false
		app.relayout()
	})
	return dispatchOf(
		bind[keyRun](actSelectCancel, closeSkills),
		bind[keyRun](actSelectAccept, closeSkills),
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
	)
}

func (m *model) handleResumeKey(msg tea.KeyMsg) tea.Cmd {
	action := m.stackAction(msg, ctxResume)
	if m.overlayNav(action, m.moveResume) {
		return nil
	}
	if run, ok := m.tables().resume[action]; ok {
		return run(m, msg)
	}
	if action != "" {
		return nil
	}
	switch msg.Type {
	case tea.KeyBackspace:
		m.eraseResume()
	case tea.KeySpace:
		m.typeResume(" ")
	case tea.KeyRunes:
		m.typeResume(string(msg.Runes))
	}
	return nil
}

func resumeRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actSelectCancel, stepKey(func(app *model) {
			app.resumeOpen = false
			app.relayout()
		})),
		bind[keyRun](actSelectAccept, cmdKey(func(app *model) tea.Cmd {
			cmd := app.chooseResume()
			app.resumeOpen = false
			app.relayout()
			return tea.Batch(cmd, app.ensureSpin())
		})),
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
	)
}

func (m *model) handlePaletteKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if !m.paletteOpen() {
		return nil, false
	}
	action := m.stackAction(msg, ctxAutocomplete)
	if m.overlayNav(action, m.movePal) {
		return nil, true
	}
	if run, ok := m.tables().palette[action]; ok {
		return run(m, msg)
	}
	return nil, false
}

func paletteRuns() (map[keyAction]chatRun, error) {
	return dispatchOf(
		bind[chatRun](actAutoAccept, stepChat(func(app *model) {
			app.completePal()
			app.relayout()
		})),
		bind[chatRun](actAutoDismiss, stepChat(func(app *model) {
			app.input.SetValue("")
			app.palIdx = 0
			app.relayout()
		})),
		bind[chatRun](actSelectAccept, func(app *model, _ tea.KeyMsg) (tea.Cmd, bool) {
			cmd := app.handleEnter()
			app.palIdx = 0
			app.relayout()
			return tea.Batch(cmd, app.ensureSpin()), true
		}),
		bind[chatRun](actInterrupt, chatCmd((*model).overlayCtrlC)),
	)
}

func (m *model) handleMentionKey(msg tea.KeyMsg) bool {
	if !m.mentionOpen() {
		return false
	}
	chosen := m.stackAction(msg, ctxMention)
	if run, ok := m.tables().mention[chosen]; ok {
		return run(m, msg)
	}
	return false
}

func mentionRuns() (map[keyAction]flagRun, error) {
	return dispatchOf(
		bind[flagRun](actSelectPrevious, stepFlag(func(app *model) { app.moveMention(-1) })),
		bind[flagRun](actSelectNext, stepFlag(func(app *model) { app.moveMention(1) })),
		bind[flagRun](actAutoAccept, stepFlag(func(app *model) {
			app.completeActiveMention()
			app.relayout()
		})),
	)
}
