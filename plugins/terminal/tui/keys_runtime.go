package tui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	if len(load.problems) > 0 {
		m.flash = "raccourcis : " + joinProblems(load.problems)
	}
}

func (m *model) pressChatKey(msg tea.KeyMsg, now time.Time) (keyAction, bool) {
	if m.bindings == nil {
		return "", false
	}
	if now.Sub(m.keyChord.started) > m.keyTimeout {
		m.keyChord = keyPress{}
	}
	textual := msg.Type == tea.KeyRunes && !msg.Alt
	if textual && m.keyChord.pending == "" {
		return "", false
	}
	key, err := canonicalKey(msg.String())
	if err != nil {
		return "", false
	}
	return m.bindings.press(chatKeyStack(), &m.keyChord, key, now, m.keyTimeout)
}

func (m *model) runChatAction(action keyAction, msg tea.KeyMsg) (tea.Cmd, bool) {
	switch action {
	case actInterrupt:
		return m.interruptOrQuit()
	case actExit:
		if m.input.Value() != "" {
			return nil, false
		}
		return tea.Quit, true
	case actCancel:
		m.escapeKey()
		return nil, true
	case actSubmit, actQueueRedirect:
		return m.enterKey(msg)
	case actNewline:
		return nil, false
	case actCycleMode:
		return m.cycleMode(), true
	case actTabsNext:
		m.switchTab(1)
		return nil, true
	case actHistoryPrevious:
		return m.upKey(msg)
	case actHistoryNext:
		return m.handleHistoryKey(msg, 1)
	case actImagePaste:
		return nil, m.pasteImage()
	case actRemovePending:
		return m.removePendingKey()
	case actStash:
		m.stashDraft()
		return nil, true
	case actClearInput:
		m.input.Reset()
		m.relayout()
		return nil, true
	case actChatSearch:
		m.openSearch()
		return nil, true
	case actHistorySearch:
		m.openHistSearch()
		return nil, true
	case actOpenSessions:
		m.openSwitch()
		return nil, true
	case actCloseTab:
		return m.closeTabKey()
	case actOpenBoard:
		return m.openBoard(), true
	case actExternalEditor:
		return m.openEditor(), true
	case actQueueSubmit, actSendNow:
		return m.sendQueueNow(), true
	case actCopyCode:
		m.copyLastCode()
		return nil, true
	case actCopyReply:
		m.copyTarget(copyReply)
		return nil, true
	case actToggleCodeFold:
		m.toggleFold()
		return nil, true
	case actToggleToolExpand:
		m.toggleToolExpand()
		return nil, true
	case actToggleTurnFold:
		m.toggleTurnFold()
		return nil, true
	case actOpenOrFold:
		m.openOrFold()
		return nil, true
	case actNextLink:
		m.selectNextLink()
		return nil, true
	case actNextTurn:
		m.jumpTurn(1)
		return nil, true
	case actPreviousTurn:
		m.jumpTurn(-1)
		return nil, true
	case actToggleMouse:
		return m.toggleMouse(), true
	case actOpenDiff:
		return m.openDiff(false), true
	case actOpenTheme:
		m.openTheme()
		return nil, true
	case actToggleSidebar:
		m.toggleSidebar()
		return nil, true
	case actToggleTodos, actToggleTranscript, actToggleBrief, actDiffFileUp, actDiffFileDown, actCycleDiffBase,
		actOpenArtifact, actOpenPanes, actKillAgents, actHelpToggle, actNotifyCycle, actSelectNext,
		actSelectPrevious, actSelectPageUp, actSelectPageDown, actSelectFirst, actSelectLast, actSelectAccept,
		actSelectCancel, actAutoAccept, actAutoDismiss, actAutoPrevious, actAutoNext, actPaneNext, actPanePrevious,
		actPaneZoom, actPaneSend, actPaneClose, actDiffDismiss, actDiffPrevFile, actDiffNextFile, actDiffReviewed,
		actDiffOpenFile, actDiffNote, actDiffCommit, actDiffPR, actDiffEdit, actDiffSendNote, actDiffStopEdit,
		actScrollPageUp, actScrollPageDown, actThemeEdit, actThemeSyntax, actThemeInstall, actThemeGlyphs,
		actThemeBorders, actThemeDensity, actThemeBold, actThemeItalic, actThemeAuto, actThemeCopyCommand,
		actThemeCopyConfig, actSearchClose, actSearchNext, actSearchPrevious, actBoardPanes, actContextCompact,
		actContextSync, actContextClose, actStatuslineClose, actStatuslineSource, actStatuslineUp,
		actStatuslineDown, actStatuslineToggle, actPlanSkip, actSessionClose, actQueuePrevious, actQueueNext,
		actQueueMoveUp, actQueueMoveDown, actQueueRemove, actQueueLeave, actRewindFork, actRewindUndo,
		actConfirmYes, actConfirmNo, actPluginToggle, actPluginInstall, actPluginFavorite, actPluginMarket,
		actHistoryCycleScope, actHistoryAccept, actHistoryCancel, actHistoryNextMatch, actApprovalDeny:
		return nil, true
	}
	return nil, true
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
	if m.bindings == nil {
		return ""
	}
	return m.bindings.hintFor(action, chatKeyStack()...)
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
