package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *model) handleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	m.keys.write("key type=%d alt=%v str=%q", msg.Type, msg.Alt, msg.String())
	m.flash = ""
	cmd, handled := m.dispatchKey(msg)
	if m.flash == "" {
		m.flash = m.takePendingBeatFlash()
	}
	return cmd, handled
}

func (m *model) dispatchKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if m.toast.title != "" {
		if run, ok := m.tables().toast[m.directAction(msg, ctxToast)]; ok {
			return run(m, msg), true
		}
	}
	switch {
	case m.diff.open:
		return m.handleDiffKey(msg), true
	case m.pendingClose:
		return m.handlePendingClose(msg), true
	case m.planMenu != nil:
		return m.handlePlanKey(msg), true
	case m.surface == surfaceBoard:
		return m.handleBoardKey(msg), true
	case m.switchOpen:
		return m.handleSwitchKey(msg), true
	case m.searchOpen:
		return m.handleSearchKey(msg), true
	case m.diagOpen:
		return m.handleDiagKey(msg), true
	case m.ctxOpen:
		return m.handleContextKey(msg), true
	case m.health.open:
		return m.handleHealthKey(msg), true
	case m.usage.open:
		return m.handleUsageKey(msg), true
	case m.pluginsOpen:
		return m.handlePluginsOverlayKey(msg), true
	case m.skillsOpen:
		return m.handleSkillsKey(msg), true
	case m.resumeOpen:
		return m.handleResumeKey(msg), true
	case m.themeScr != nil:
		return m.handleThemeKey(msg), true
	case m.bar.open:
		return m.handleStatuslineKey(msg), true
	case m.histOpen:
		return m.handleHistSearchKey(msg), true
	case m.rwOpen:
		return m.handleRewindKey(msg), true
	}
	if m.handleApprovalKey(msg) {
		return nil, true
	}
	if m.surface == surfacePanes {
		if cmd, handled := m.handlePanesKey(msg); handled {
			return cmd, true
		}
	}
	if cmd, handled := m.handlePaletteKey(msg); handled {
		return cmd, true
	}
	if m.handleMentionKey(msg) {
		return nil, true
	}
	return m.handleMainKey(msg)
}

func (m *model) handlePendingClose(msg tea.KeyMsg) tea.Cmd {
	action := m.stackAction(msg, ctxClose)
	m.pendingClose = false
	if run, ok := m.tables().pendingClose[action]; ok {
		return run(m, msg)
	}
	return nil
}

func pendingCloseRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actSessionClose, cmdKey(func(app *model) tea.Cmd { return app.confirmClose() })),
		bind[keyRun](actInterrupt, cmdKey(func(app *model) tea.Cmd {
			cmd, _ := app.interruptOrQuit(actInterrupt)
			return cmd
		})),
		bind[keyRun](actExit, cmdKey(func(app *model) tea.Cmd {
			cmd, _ := app.interruptOrQuit(actExit)
			return cmd
		})),
	)
}

func (m *model) handleMainKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if m.handleQueueFocus(msg) {
		return nil, true
	}
	action, matched := m.pressChatKey(msg, time.Now())
	if !matched {
		return nil, false
	}
	if action == "" {
		return nil, true
	}
	return m.runChatAction(action, msg)
}

func (m *model) escapeKey() {
	if tb := m.tabs[m.active]; tb != nil && tb.busy {
		if name := m.sessionName(m.active); name != "" {
			m.tm.Interrupt(name)
		}
		return
	}
	m.escapeIdle()
}

func (m *model) enterKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if msg.Alt {
		return nil, m.enqueueWhileBusy(true)
	}
	if m.continueLine() || m.enqueueWhileBusy(false) {
		return nil, true
	}
	return tea.Batch(m.handleEnter(), m.ensureSpin()), true
}

func (m *model) removePendingKey() (tea.Cmd, bool) {
	if !m.removeLastPending() {
		return nil, false
	}
	m.relayout()
	return nil, true
}

func (m *model) upKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if m.focusQueueStart() {
		return nil, true
	}
	return m.handleHistoryKey(msg, -1)
}

func (m *model) closeTabKey() (tea.Cmd, bool) {
	if m.input.Value() != "" {
		return nil, false
	}
	m.pendingClose = true
	return nil, true
}

const quitArmWindow = 3 * time.Second

func (m *model) interruptOrQuit(pressed keyAction) (tea.Cmd, bool) {
	turn := m.tabs[m.active]
	if turn != nil && turn.busy && !turn.interruptRequested {
		turn.interruptRequested = true
		if !m.interruptActive() {
			m.flash = m.tr.t("tui.status.nothing_to_interrupt", map[string]any{tplArgKey: m.hintOf(pressed)})
		}
		return nil, true
	}
	if busy := m.busySessions(); busy > 0 && time.Now().After(m.quitArmedUntil) {
		m.quitArmedUntil = time.Now().Add(quitArmWindow)
		m.flash = m.tr.t("tui.status.quit_busy", map[string]any{tplArgCount: busy, tplArgKey: m.hintOf(pressed)})
		return nil, true
	}
	return tea.Quit, true
}

func (m *model) busySessions() int {
	busy := 0
	for name, tb := range m.tabs {
		if tb.busy && (name != m.active || !tb.interruptRequested) {
			busy++
		}
	}
	return busy
}

func (m *model) interruptActive() bool {
	name := m.sessionName(m.active)
	return name != "" && m.tm.Interrupt(name)
}

func (m *model) overlayCtrlC() tea.Cmd {
	cmd, _ := m.interruptOrQuit(actInterrupt)
	return cmd
}

func (m *model) handleHistoryKey(_ tea.KeyMsg, dir int) (tea.Cmd, bool) {
	if dir < 0 {
		if m.input.LineCount() <= 1 && (m.input.Value() == "" || m.histIdx < len(m.history)) && m.recallHistory(-1) {
			return nil, true
		}
		return nil, false
	}
	if m.input.LineCount() <= 1 && m.histIdx < len(m.history) && m.recallHistory(1) {
		return nil, true
	}
	return nil, false
}
