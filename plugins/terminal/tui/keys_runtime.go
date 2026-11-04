package tui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func chatKeyStack() []keyContext {
	return []keyContext{ctxChat, ctxGlobal}
}

type helpEntry struct {
	action keyAction
	label  string
}

func helpEntries() []helpEntry {
	return []helpEntry{
		{actSubmit, "envoyer"},
		{actQueueRedirect, "réoriente"},
		{actQueueSubmit, "envoie la file"},
		{actCancel, "interrompre"},
		{actImagePaste, "coller une image"},
		{actExternalEditor, "éditeur"},
		{actToggleMouse, "libérer la souris"},
		{actNextLink, "lien suivant"},
		{actOpenOrFold, "l’ouvrir"},
		{actCopyCode, "copier le code"},
		{actCopyReply, "copier la réponse"},
		{actToggleCodeFold, "replier le code"},
		{actToggleToolExpand, "déplier commandes et diffs"},
		{actToggleTurnFold, "replier les tours"},
		{actChatSearch, "chercher"},
		{actStash, "brouillon de côté"},
		{actHistorySearch, "recherche historique"},
		{actOpenSessions, "sessions"},
	}
}

func (m *model) loadKeys(configDir string) {
	section, err := readKeysSection(filepath.Join(configDir, terminalConfigFile))
	if err != nil {
		m.flash = "terminal.toml [keys] ignoré : " + err.Error()
		section = keysSection{}
	}
	claudePath := ""
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		claudePath = filepath.Join(home, ".claude", keybindingsFile)
	}
	load := loadKeyBindings(claudePath, filepath.Join(configDir, keybindingsFile), section)
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
	return nil, true
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
		m.flash = "aucune session occupée"
		return
	}
	m.flash = "sessions interrompues : " + strconv.Itoa(count)
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
			entries = append(entries, glyph+" "+entry.label)
		}
	}
	if newline := m.hintsOf(actNewline); newline != "" {
		entries = append(entries, newline+" ou \\⏎ nouvelle ligne")
	}
	for _, turn := range []helpEntry{{actPreviousTurn, "tour précédent"}, {actNextTurn, "tour suivant"}} {
		if glyph := m.hintOf(turn.action); glyph != "" {
			entries = append(entries, glyph+" "+turn.label)
		}
	}
	entries = append(entries, "/ commandes", "@ fichiers", "! commande shell")
	return strings.Join(entries, " · ")
}

func (m *model) hintOf(action keyAction) string {
	return m.hintIn(action, chatKeyStack()...)
}

func (m *model) hintIn(action keyAction, stack ...keyContext) string {
	return m.activeBindings().hintFor(action, stack...)
}

func (m *model) activeBindings() *keyTable {
	if m.bindings != nil {
		return m.bindings
	}
	return defaultKeyTable()
}

func defaultKeyTable() *keyTable {
	return loadKeyBindings("", "", keysSection{}).table
}

func (m *model) hintsOf(action keyAction) string {
	if m.bindings == nil {
		return ""
	}
	glyphs := make([]string, 0, len(m.bindings.chordsOf(action, chatKeyStack()...)))
	for _, chord := range m.bindings.chordsOf(action, chatKeyStack()...) {
		glyphs = append(glyphs, keyHintGlyph(chord))
	}
	return strings.Join(glyphs, " ou ")
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
		m.flash = "dossier de configuration inconnu"
		return nil
	}
	path := filepath.Join(m.themeDir, keybindingsFile)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(m.themeDir, privateDirMode); err != nil {
			m.flash = "keybindings.json impossible : " + err.Error()
			return nil
		}
		if err := os.WriteFile(path, keybindingsSkeleton(), privateFileMode); err != nil {
			m.flash = "keybindings.json impossible : " + err.Error()
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
		m.flash = "éditeur fermé en erreur : " + msg.err.Error()
	}
	m.loadKeys(m.themeDir)
	if len(m.keyProblems) == 0 && msg.err == nil {
		m.flash = "raccourcis rechargés"
	}
}
