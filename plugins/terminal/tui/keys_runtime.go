package tui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func chatKeyStack() []keyContext {
	return []keyContext{ctxChat, ctxGlobal}
}

type helpEntry struct {
	action   keyAction
	labelKey string
}

func helpEntries() []helpEntry {
	return []helpEntry{
		{actSubmit, "tui.common.send"},
		{actQueueRedirect, "tui.keys.help.redirect"},
		{actQueueSubmit, "tui.keys.help.queue_submit"},
		{actCancel, "tui.common.interrupt"},
		{actImagePaste, "tui.keys.help.image_paste"},
		{actExternalEditor, "tui.common.editor"},
		{actToggleMouse, "tui.keys.help.mouse"},
		{actNextLink, "tui.keys.help.next_link"},
		{actOpenOrFold, "tui.keys.help.open"},
		{actCopyCode, "tui.keys.help.copy_code"},
		{actCopyReply, "tui.keys.help.copy_reply"},
		{actToggleCodeFold, "tui.keys.help.fold_code"},
		{actToggleToolExpand, "tui.keys.help.tool_expand"},
		{actToggleTurnFold, "tui.keys.help.fold_turns"},
		{actChatSearch, "tui.keys.help.search"},
		{actStash, "tui.keys.help.stash"},
		{actHistorySearch, "tui.keys.help.history"},
		{actOpenSessions, "tui.keys.help.sessions"},
	}
}

func (m *model) loadKeys(configDir string) {
	section, err := readKeysSection(filepath.Join(configDir, terminalConfigFile))
	if err != nil {
		m.flash = m.tr.t("tui.keys.ignored", map[string]any{tplArgReason: err.Error()})
		section = keysSection{}
	}
	claudePath := ""
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		claudePath = filepath.Join(home, ".claude", keybindingsFile)
	}
	load := loadKeyBindings(m.tr, claudePath, filepath.Join(configDir, keybindingsFile), section)
	m.bindings = load.table
	m.keyTimeout = chordTimeout(load.timeout)
	m.keyChord = keyPress{}
	m.keyProblems = load.problems
	m.installTables()
	m.syncComposerKeys()
	if len(m.keyProblems) > 0 {
		m.flash = joinProblems(m.keyProblems)
	}
}

func (m *model) syncComposerKeys() {
	m.input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys(m.newlineKeys()...))
}

func (m *model) newlineKeys() []string {
	var keys []string
	for _, action := range []keyAction{actNewline, actQueueRedirect} {
		for _, chord := range m.activeBindings().chordsOf(action, chatKeyStack()...) {
			if !strings.Contains(chord, " ") {
				keys = append(keys, chord)
			}
		}
	}
	return keys
}

func (m *model) stackAction(msg tea.KeyMsg, stack ...keyContext) keyAction {
	if m.bindings == nil {
		return ""
	}
	now := time.Now()
	if now.Sub(m.keyChord.started) > m.keyTimeout {
		m.keyChord = keyPress{}
	}
	chord, err := canonicalKey(msg.String())
	if err != nil {
		return ""
	}
	action, _ := m.bindings.press(stack, &m.keyChord, chord, now, m.keyTimeout)
	return action
}

func (m *model) pressChatKey(msg tea.KeyMsg, now time.Time) (keyAction, bool) {
	if m.bindings == nil {
		return "", false
	}
	if now.Sub(m.keyChord.started) > m.keyTimeout {
		m.keyChord = keyPress{}
	}
	chord, err := canonicalKey(msg.String())
	if err != nil {
		return "", false
	}
	return m.bindings.press(chatKeyStack(), &m.keyChord, chord, now, m.keyTimeout)
}

func (m *model) runChatAction(action keyAction, msg tea.KeyMsg) (tea.Cmd, bool) {
	if run, ok := m.tables().chat[action]; ok {
		return run(m, msg)
	}
	m.flash = m.unavailableNotice(action)
	return nil, true
}

func movedActions() map[keyAction]keyAction {
	moved := make(map[keyAction]keyAction)
	moved[actToggleTodos] = actOpenSessions
	moved[actChatNewerModel] = actCopyCode
	moved[actTaskBackground] = actToggleSidebar
	moved[actChatThinking] = actToggleTurnFold
	moved[actDiffFileUp] = actPreviousTurn
	moved[actDiffFileDown] = actNextTurn
	return moved
}

func (m *model) unavailableNotice(action keyAction) string {
	target, moved := movedActions()[action]
	if !moved {
		return m.tr.t("tui.keys.unavailable", map[string]any{tplArgAction: string(action)})
	}
	chord := m.hintOf(target)
	if chord == "" {
		return m.tr.t("tui.keys.unavailable", map[string]any{tplArgAction: string(action)})
	}
	return m.tr.t("tui.keys.unavailable_moved", map[string]any{tplArgAction: string(action), "Chord": chord})
}

func chatRuns() (map[keyAction]chatRun, error) {
	toggleTool := stepChat((*model).toggleToolExpand)
	sendQueue := chatCmd((*model).sendQueueNow)
	return dispatchOf(
		bind[chatRun](actInterrupt, func(app *model, _ tea.KeyMsg) (tea.Cmd, bool) {
			return app.interruptOrQuit(actInterrupt)
		}),
		bind[chatRun](actExit, func(app *model, _ tea.KeyMsg) (tea.Cmd, bool) {
			if app.input.Value() != "" {
				return nil, false
			}
			return app.interruptOrQuit(actExit)
		}),
		bind[chatRun](actCancel, stepChat((*model).escapeKey)),
		bind[chatRun](actSubmit, func(app *model, msg tea.KeyMsg) (tea.Cmd, bool) {
			return app.enterKey(msg)
		}),
		bind[chatRun](actQueueRedirect, func(app *model, _ tea.KeyMsg) (tea.Cmd, bool) {
			return nil, app.enqueueWhileBusy(true)
		}),
		bind[chatRun](actNewline, func(*model, tea.KeyMsg) (tea.Cmd, bool) {
			return nil, false
		}),
		bind[chatRun](actCycleMode, chatCmd((*model).cycleMode)),
		bind[chatRun](actTabsNext, stepChat(func(app *model) { app.switchTab(1) })),
		bind[chatRun](actHistoryPrevious, func(app *model, msg tea.KeyMsg) (tea.Cmd, bool) {
			return app.upKey(msg)
		}),
		bind[chatRun](actHistoryNext, func(app *model, msg tea.KeyMsg) (tea.Cmd, bool) {
			return app.handleHistoryKey(msg, 1)
		}),
		bind[chatRun](actImagePaste, func(app *model, _ tea.KeyMsg) (tea.Cmd, bool) {
			return nil, app.pasteImage()
		}),
		bind[chatRun](actRemovePending, func(app *model, _ tea.KeyMsg) (tea.Cmd, bool) {
			return app.removePendingKey()
		}),
		bind[chatRun](actStash, stepChat((*model).stashDraft)),
		bind[chatRun](actClearInput, stepChat(func(app *model) { app.input.Reset(); app.relayout() })),
		bind[chatRun](actChatUndo, func(app *model, _ tea.KeyMsg) (tea.Cmd, bool) {
			return nil, app.undoComposer()
		}),
		bind[chatRun](actChatSearch, stepChat((*model).openSearch)),
		bind[chatRun](actHistorySearch, stepChat((*model).openHistSearch)),
		bind[chatRun](actOpenSessions, stepChat((*model).openSwitch)),
		bind[chatRun](actCloseTab, func(app *model, _ tea.KeyMsg) (tea.Cmd, bool) {
			return app.closeTabKey()
		}),
		bind[chatRun](actOpenBoard, chatCmd((*model).openBoard)),
		bind[chatRun](actExternalEditor, chatCmd((*model).openEditor)),
		bind[chatRun](actQueueSubmit, sendQueue),
		bind[chatRun](actSendNow, sendQueue),
		bind[chatRun](actCopyCode, stepChat((*model).copyLastCode)),
		bind[chatRun](actCopyReply, stepChat(func(app *model) { app.copyTarget(copyReply) })),
		bind[chatRun](actToggleCodeFold, stepChat((*model).toggleFold)),
		bind[chatRun](actToggleToolExpand, toggleTool),
		bind[chatRun](actToggleTranscript, toggleTool),
		bind[chatRun](actToggleTurnFold, stepChat((*model).toggleTurnFold)),
		bind[chatRun](actOpenOrFold, stepChat((*model).openOrFold)),
		bind[chatRun](actNextLink, stepChat((*model).selectNextLink)),
		bind[chatRun](actNextTurn, stepChat(func(app *model) { app.jumpTurn(1) })),
		bind[chatRun](actPreviousTurn, stepChat(func(app *model) { app.jumpTurn(-1) })),
		bind[chatRun](actToggleMouse, chatCmd((*model).toggleMouse)),
		bind[chatRun](actOpenDiff, func(app *model, _ tea.KeyMsg) (tea.Cmd, bool) {
			return app.openDiff(false), true
		}),
		bind[chatRun](actOpenTheme, stepChat((*model).openTheme)),
		bind[chatRun](actToggleSidebar, stepChat((*model).toggleSidebar)),
		bind[chatRun](actKillAgents, stepChat((*model).interruptBusySessions)),
		bind[chatRun](actHelpToggle, func(app *model, _ tea.KeyMsg) (tea.Cmd, bool) {
			if app.input.Value() != "" {
				return nil, false
			}
			app.toggleHelp()
			return nil, true
		}),
		bind[chatRun](actNotifyCycle, func(app *model, _ tea.KeyMsg) (tea.Cmd, bool) {
			if app.input.Value() != "" || app.toast.title == "" {
				return nil, false
			}
			return app.cycleNotify(), true
		}),
	)
}

func (m *model) interruptBusySessions() {
	count := 0
	for channel, tb := range m.tabs {
		if tb == nil || !tb.busy {
			continue
		}
		if name := m.sessionName(channel); name != "" {
			m.tm.Interrupt(name)
			count++
		}
	}
	if count == 0 {
		m.flash = m.tr.t("tui.keys.none_busy", nil)
		return
	}
	m.flash = m.tr.t("tui.keys.interrupted", map[string]any{tplArgCount: count})
}

func (m *model) undoComposer() bool {
	last := len(m.composerUndo) - 1
	if last < 0 {
		return false
	}
	m.input.SetValue(m.composerUndo[last])
	m.composerUndo = m.composerUndo[:last]
	m.resizeComposer()
	m.relayout()
	return true
}

func (m *model) rememberComposer(previous string) {
	const composerUndoDepth = 200
	m.composerUndo = append(m.composerUndo, previous)
	if over := len(m.composerUndo) - composerUndoDepth; over > 0 {
		m.composerUndo = m.composerUndo[over:]
	}
}

func (m *model) directAction(msg tea.KeyMsg, stack ...keyContext) keyAction {
	if m.bindings == nil {
		return ""
	}
	chord, err := canonicalKey(msg.String())
	if err != nil {
		return ""
	}
	action, _ := m.bindings.lookup(stack, chord)
	return action
}

func (m *model) keyHelpLine() string {
	var entries []string
	for _, entry := range helpEntries() {
		if glyph := m.hintOf(entry.action); glyph != "" {
			entries = append(entries, glyph+" "+m.tr.t(entry.labelKey, nil))
		}
	}
	if newline := m.hintsOf(actNewline); newline != "" {
		line := m.tr.t("tui.keys.newline", map[string]any{"Keys": newline})
		if idle := m.hintsOf(actQueueRedirect); idle != "" {
			line += m.tr.t("tui.keys.newline_idle", map[string]any{"Keys": idle})
		}
		entries = append(entries, line)
	}
	for _, turn := range []helpEntry{{actPreviousTurn, "tui.keys.help.previous_turn"}, {actNextTurn, "tui.keys.help.next_turn"}} {
		if glyph := m.hintOf(turn.action); glyph != "" {
			entries = append(entries, glyph+" "+m.tr.t(turn.labelKey, nil))
		}
	}
	entries = append(entries, m.tr.t("tui.keys.commands", nil), m.tr.t("tui.keys.files", nil), m.tr.t("tui.keys.shell", nil))
	return strings.Join(entries, " · ")
}

func (m *model) hintOf(action keyAction) string {
	return m.hintIn(action, chatKeyStack()...)
}

func (m *model) hintIn(action keyAction, stack ...keyContext) string {
	return m.activeBindings().hintFor(m.tr, action, stack...)
}

func (m *model) activeBindings() *keyTable {
	if m.bindings != nil {
		return m.bindings
	}
	return defaultKeyTable(m.tr)
}

func defaultKeyTable(trans translator) *keyTable {
	return loadKeyBindings(trans, "", "", keysSection{}).table
}

func (m *model) hintsOf(action keyAction) string {
	if m.bindings == nil {
		return ""
	}
	glyphs := make([]string, 0, len(m.bindings.chordsOf(action, chatKeyStack()...)))
	for _, chord := range m.bindings.chordsOf(action, chatKeyStack()...) {
		glyphs = append(glyphs, keyHintGlyph(m.tr, chord))
	}
	return strings.Join(glyphs, " "+m.tr.t("tui.keys.or", nil)+" ")
}

func (m *model) labelledHint(action keyAction, label string, stack ...keyContext) string {
	glyph := m.hintIn(action, stack...)
	if glyph == "" {
		return ""
	}
	return glyph + " " + label
}

func (m *model) pairHint(previous, next keyAction, label string, stack ...keyContext) string {
	glyphs := joinWith("", m.hintIn(previous, stack...), m.hintIn(next, stack...))
	return strings.TrimSpace(joinWith(" ", glyphs, label))
}

func joinWith(separator string, parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, separator)
}

type keybindingsDoneMsg struct {
	err error
}

func (m *model) openKeybindings() tea.Cmd {
	if m.themeDir == "" {
		m.flash = m.tr.t("tui.keys.no_config_dir", nil)
		return nil
	}
	path := filepath.Join(m.themeDir, keybindingsFile)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(m.themeDir, privateDirMode); err != nil {
			m.flash = m.tr.t("tui.keys.file_failed", map[string]any{tplArgReason: err.Error()})
			return nil
		}
		if err := os.WriteFile(path, keybindingsSkeleton(), privateFileMode); err != nil {
			m.flash = m.tr.t("tui.keys.file_failed", map[string]any{tplArgReason: err.Error()})
			return nil
		}
	}
	m.editing = true
	return tea.ExecProcess(editorCommand(m.runContext(), path), func(err error) tea.Msg {
		return keybindingsDoneMsg{err: err}
	})
}

func (m *model) applyKeybindingsDone(msg keybindingsDoneMsg) {
	m.editing = false
	if msg.err != nil {
		m.flash = m.tr.t("tui.editor.closed_error", map[string]any{tplArgReason: msg.err.Error()})
	}
	m.loadKeys(m.themeDir)
	if len(m.keyProblems) == 0 && msg.err == nil {
		m.flash = m.tr.t("tui.keys.reloaded", nil)
	}
}
