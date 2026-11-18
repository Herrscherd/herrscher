package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *model) ensureSpin() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return spinTick(fastTick)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.dispatch(msg)
	m.syncComposer()
	m.resizeComposer()
	m.applySize()
	return model, tea.Batch(cmd, m.drainHistory())
}

func (m *model) dispatch(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled := m.handlePluginsMsg(msg); handled {
		return m, nil
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if cmd, handled := m.handleKey(keyMsg); handled {
			return m, cmd
		}
	} else if cmd, handled := m.handleMsg(msg); handled {
		return m, cmd
	}
	cmd := m.updateComponents(msg)
	return m, cmd
}

func (m *model) relayout() {
	m.syncComposer()
	m.resizeComposer()
	m.applySize()
	m.syncViewport()
}

func (m *model) handleMsg(msg tea.Msg) (tea.Cmd, bool) {
	if cmd, handled := m.handleDiffMsg(msg); handled {
		return cmd, true
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.handleWindowSize(msg)
	case eventMsg:
		cmd := m.route(RoutedEvent(msg))
		return tea.Batch(cmd, m.ensureSpin()), true
	case tea.MouseMsg:
		if m.handlePaneClick(msg) {
			return nil, true
		}
		if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft && m.clickTranscript(msg) {
			return nil, true
		}
		if cmd, handled := m.handleModeClick(msg); handled {
			return cmd, true
		}
		if m.handleQueueClick(msg) {
			return nil, true
		}
	case imageReadyMsg:
		m.applyImage(msg)
		return nil, true
	case themeWatchMsg:
		m.watchThemes()
		return themeWatchTick(), true
	case themeEditedMsg:
		m.handleThemeEdited(msg)
		return nil, true
	case enhancedKeysFailedMsg:
		m.flash = m.tr.t("tui.status.enhanced_keys_unavailable", map[string]any{tplArgReason: localizedReason(m.tr, msg.err)})
		return nil, true
	case dispatchResultMsg:
		return m.handleDispatchResult(msg), true
	case sessionStartedMsg:
		return m.handleSessionStarted(msg), true
	case modeResultMsg:
		return m.handleModeResult(msg), true
	case modeLoadedMsg:
		m.handleModeLoaded(msg)
		return nil, true
	case shellResultMsg:
		m.handleShellResult(msg)
		return nil, true
	case signalFailedMsg:
		m.flash = m.tr.t("tui.status.notify_failed", map[string]any{tplArgReason: localizedReason(m.tr, msg.err)})
		return nil, true
	case editorDoneMsg:
		m.applyEditorResult(msg)
		return nil, true
	case keybindingsDoneMsg:
		m.applyKeybindingsDone(msg)
		return nil, true
	case draftBeatFailedMsg:
		m.queueBeatFlash(msg.err)
		return m.watchDraftBeat(), true
	case compactResultMsg:
		m.handleCompactResult(msg)
		return nil, true
	case healthMsg:
		m.handleHealthMsg(msg)
		return nil, true
	case usageMsg:
		m.handleUsageMsg(msg)
		return nil, true
	case toastExpiredMsg:
		m.handleToastExpired(msg)
		return nil, true
	case toastTickMsg:
		return m.handleToastTick(msg), true
	case tea.FocusMsg:
		m.handleFocus(true)
		return nil, true
	case tea.BlurMsg:
		m.handleFocus(false)
		return nil, true
	case rewindDoneMsg:
		m.handleRewindDone(msg)
		return nil, true
	case forkDoneMsg:
		return m.handleForkDone(msg), true
	case forkResolvedMsg:
		m.handleForkResolved(msg)
		return nil, true
	case scrollbackMsg:
		return m.handleScrollback(msg), true
	case tickMsg:
		syncCmd := m.refreshTabs()
		m.expireApprovals(time.Now())
		surfaceCmd := m.refreshSurfaceCmd()
		return tea.Batch(syncCmd, surfaceCmd, tickCmd(), m.gitTick(), m.loadModesCmd(), m.summaryRefresh(), m.healthTick()), true
	case sessionsMsg:
		m.sessionsInFlight = false
		if msg.seq < m.sessionsApplied {
			return nil, true
		}
		m.sessionsApplied = msg.seq
		return m.applySessions(msg.infos), true
	case surfaceMsg:
		m.applySurface(msg)
		return nil, true
	case summaryMsg:
		m.summaryBusy = false
		m.summary = panelSummary(msg)
		return nil, true
	case spinMsg:
		return m.handleSpin(), true
	case fontProbeMsg:
		m.obelFont = msg.presence
		return nil, true
	case scriptTickMsg:
		return m.handleScriptTick(), true
	case scriptDoneMsg:
		m.receiveScript(msg)
		return nil, true
	}
	return nil, false
}

func (m *model) handleWindowSize(msg tea.WindowSizeMsg) {
	m.width, m.height = msg.Width, msg.Height
	if !m.ready {
		m.vp = viewport.New(msg.Width, msg.Height-m.chromeHeight())
		m.ready = true
	} else {
		m.applySize()
	}
	m.input.SetWidth(m.composerTextWidth())
	m.resizeComposer()
	m.syncViewport()
}

func (m *model) handleDispatchResult(msg dispatchResultMsg) tea.Cmd {
	cmd := m.refreshTabs()
	line, role := msg.out, roleNotice
	if msg.err != nil {
		line, role = msg.err.Error(), roleError
	}
	if line == "" {
		return cmd
	}
	tab := m.tabs[msg.origin]
	if tab == nil {
		tab = m.tabs[m.active]
	}
	if tab == nil {
		m.flash = line
		return cmd
	}
	tab.appendEntry(entry{role: role, text: line})
	m.syncViewport()
	return cmd
}

func (m *model) handleSpin() tea.Cmd {
	if !m.anyBusy() {
		m.spinning = false
		return nil
	}
	m.spin++
	m.syncViewport()
	return spinTick(fastTick)
}

func (m *model) updateComponents(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	previous := m.input.Value()
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != previous {
		m.rememberComposer(previous)
	}
	cmds = append(cmds, cmd)
	m.vp, cmd = m.vp.Update(msg)
	cmds = append(cmds, cmd)
	if _, ok := msg.(tea.KeyMsg); ok {
		m.resizeComposer()
		if m.paletteOpen() || m.palWasOpen {
			m.clampPal()
			m.relayout()
		}
		m.palWasOpen = m.paletteOpen()
	}
	return tea.Batch(cmds...)
}
