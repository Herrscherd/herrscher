package tui

import "slices"

const (
	ctxGlobal          keyContext = "Global"
	ctxChat            keyContext = "Chat"
	ctxAutocomplete    keyContext = "Autocomplete"
	ctxHistorySearch   keyContext = "HistorySearch"
	ctxMessageSelector keyContext = "MessageSelector"
	ctxThemePicker     keyContext = "ThemePicker"
	ctxDiffPanel       keyContext = "DiffPanel"
	ctxDiffDialog      keyContext = "DiffDialog"
	ctxPane            keyContext = "Pane"
	ctxPlugin          keyContext = "Plugin"
	ctxSelect          keyContext = "Select"
	ctxScroll          keyContext = "Scroll"
	ctxConfirmation    keyContext = "Confirmation"
	ctxTask            keyContext = "Task"
	ctxFooter          keyContext = "Footer"
	ctxAbovePrompt     keyContext = "AbovePrompt"
	ctxAbovePromptIn   keyContext = "AbovePromptInput"
	ctxAbovePromptSel  keyContext = "AbovePromptSelect"
	ctxTabs            keyContext = "Tabs"
	ctxAttachments     keyContext = "Attachments"
	ctxHelp            keyContext = "Help"
	ctxTranscript      keyContext = "Transcript"
	ctxSettings        keyContext = "Settings"
	ctxModelPicker     keyContext = "ModelPicker"
	ctxEffortSlider    keyContext = "EffortSlider"
	ctxAgents          keyContext = "Agents"
	ctxBoard           keyContext = "Board"
	ctxSessions        keyContext = "Sessions"
	ctxSearch          keyContext = "Search"
	ctxDiagnostics     keyContext = "Diagnostics"
	ctxContext         keyContext = "Context"
	ctxStatusline      keyContext = "Statusline"
	ctxPlan            keyContext = "Plan"
	ctxSkills          keyContext = "Skills"
	ctxResume          keyContext = "Resume"
	ctxClose           keyContext = "Close"
	ctxQueue           keyContext = "Queue"
	ctxApproval        keyContext = "Approval"
	ctxApprovalReason  keyContext = "ApprovalReason"
	ctxMention         keyContext = "Mention"
	ctxThemeInstall    keyContext = "ThemeInstall"
	ctxDiffNote        keyContext = "DiffNote"
	ctxDiffMessage     keyContext = "DiffMessage"
)

const (
	actInterrupt         keyAction = "app:interrupt"
	actExit              keyAction = "app:exit"
	actToggleTodos       keyAction = "app:toggleTodos"
	actToggleTranscript  keyAction = "app:toggleTranscript"
	actToggleBrief       keyAction = "app:toggleBrief"
	actDiffFileUp        keyAction = "app:diffFileListUp"
	actDiffFileDown      keyAction = "app:diffFileListDown"
	actCycleDiffBase     keyAction = "app:cycleDiffBase"
	actOpenArtifact      keyAction = "app:openArtifact"
	actOpenBoard         keyAction = "app:openBoard"
	actOpenPanes         keyAction = "app:openPanes"
	actOpenDiff          keyAction = "app:openDiff"
	actOpenSessions      keyAction = "app:openSessions"
	actOpenTheme         keyAction = "app:openTheme"
	actToggleMouse       keyAction = "app:toggleMouse"
	actToggleSidebar     keyAction = "app:toggleSidebar"
	actCloseTab          keyAction = "app:closeTab"
	actHistorySearch     keyAction = "history:search"
	actHistoryPrevious   keyAction = "history:previous"
	actHistoryNext       keyAction = "history:next"
	actTabsNext          keyAction = "tabs:next"
	actCancel            keyAction = "chat:cancel"
	actSubmit            keyAction = "chat:submit"
	actKillAgents        keyAction = "chat:killAgents"
	actCycleMode         keyAction = "chat:cycleMode"
	actQueueSubmit       keyAction = "chat:queueSubmit"
	actSendNow           keyAction = "chat:sendNow"
	actNewline           keyAction = "chat:newline"
	actExternalEditor    keyAction = "chat:externalEditor"
	actStash             keyAction = "chat:stash"
	actImagePaste        keyAction = "chat:imagePaste"
	actClearInput        keyAction = "chat:clearInput"
	actQueueRedirect     keyAction = "chat:queueRedirect"
	actChatSearch        keyAction = "chat:search"
	actCopyCode          keyAction = "chat:copyCode"
	actCopyReply         keyAction = "chat:copyReply"
	actToggleCodeFold    keyAction = "chat:toggleCodeFold"
	actToggleToolExpand  keyAction = "chat:toggleToolExpand"
	actToggleTurnFold    keyAction = "chat:toggleTurnFold"
	actOpenOrFold        keyAction = "chat:openOrFold"
	actNextLink          keyAction = "chat:nextLink"
	actNextTurn          keyAction = "chat:nextTurn"
	actPreviousTurn      keyAction = "chat:previousTurn"
	actRemovePending     keyAction = "chat:removePending"
	actHelpToggle        keyAction = "chat:helpToggle"
	actNotifyCycle       keyAction = "chat:notifyCycle"
	actSelectNext        keyAction = "select:next"
	actSelectPrevious    keyAction = "select:previous"
	actSelectPageUp      keyAction = "select:pageUp"
	actSelectPageDown    keyAction = "select:pageDown"
	actSelectFirst       keyAction = "select:first"
	actSelectLast        keyAction = "select:last"
	actSelectAccept      keyAction = "select:accept"
	actSelectCancel      keyAction = "select:cancel"
	actAutoAccept        keyAction = "autocomplete:accept"
	actAutoDismiss       keyAction = "autocomplete:dismiss"
	actAutoPrevious      keyAction = "autocomplete:previous"
	actAutoNext          keyAction = "autocomplete:next"
	actPaneNext          keyAction = "pane:next"
	actPanePrevious      keyAction = "pane:previous"
	actPaneZoom          keyAction = "pane:zoom"
	actPaneSend          keyAction = "pane:send"
	actPaneClose         keyAction = "pane:close"
	actDiffDismiss       keyAction = "diff:dismiss"
	actDiffPrevFile      keyAction = "diff:previousFile"
	actDiffNextFile      keyAction = "diff:nextFile"
	actDiffReviewed      keyAction = "diff:toggleReviewed"
	actDiffOpenFile      keyAction = "diff:openFile"
	actDiffNote          keyAction = "diff:note"
	actDiffCommit        keyAction = "diff:commit"
	actDiffPR            keyAction = "diff:pr"
	actDiffEdit          keyAction = "diff:edit"
	actDiffSendNote      keyAction = "diff:sendNote"
	actDiffStopEdit      keyAction = "diff:stopEdit"
	actScrollPageUp      keyAction = "scroll:pageUp"
	actScrollPageDown    keyAction = "scroll:pageDown"
	actThemeEdit         keyAction = "theme:editCustom"
	actThemeSyntax       keyAction = "theme:toggleSyntaxHighlighting"
	actThemeInstall      keyAction = "theme:install"
	actThemeGlyphs       keyAction = "theme:glyphs"
	actThemeBorders      keyAction = "theme:borders"
	actThemeDensity      keyAction = "theme:density"
	actThemeBold         keyAction = "theme:bold"
	actThemeItalic       keyAction = "theme:italic"
	actThemeAuto         keyAction = "theme:auto"
	actThemeCopyCommand  keyAction = "theme:copyCommand"
	actThemeCopyConfig   keyAction = "theme:copyConfig"
	actSearchClose       keyAction = "search:close"
	actSearchNext        keyAction = "search:next"
	actSearchPrevious    keyAction = "search:previous"
	actBoardPanes        keyAction = "board:panes"
	actContextCompact    keyAction = "context:compact"
	actContextSync       keyAction = "context:syncTabs"
	actContextClose      keyAction = "context:close"
	actStatuslineClose   keyAction = "statusline:close"
	actStatuslineSource  keyAction = "statusline:switchSource"
	actStatuslineUp      keyAction = "statusline:fieldUp"
	actStatuslineDown    keyAction = "statusline:fieldDown"
	actStatuslineToggle  keyAction = "statusline:toggleField"
	actPlanSkip          keyAction = "plan:skip"
	actSessionClose      keyAction = "session:confirmClose"
	actQueuePrevious     keyAction = "queue:previous"
	actQueueNext         keyAction = "queue:next"
	actQueueMoveUp       keyAction = "queue:moveUp"
	actQueueMoveDown     keyAction = "queue:moveDown"
	actQueueRemove       keyAction = "queue:remove"
	actQueueLeave        keyAction = "queue:leave"
	actRewindFork        keyAction = "rewind:fork"
	actRewindUndo        keyAction = "rewind:undo"
	actConfirmYes        keyAction = "confirm:yes"
	actConfirmNo         keyAction = "confirm:no"
	actPluginToggle      keyAction = "plugin:toggle"
	actPluginInstall     keyAction = "plugin:install"
	actPluginFavorite    keyAction = "plugin:favorite"
	actPluginMarket      keyAction = "plugin:cycleMarketplace"
	actHistoryCycleScope keyAction = "historySearch:cycleScope"
	actHistoryAccept     keyAction = "historySearch:accept"
	actHistoryCancel     keyAction = "historySearch:cancel"
	actHistoryNextMatch  keyAction = "historySearch:next"
	actApprovalDeny      keyAction = "approval:reason"
)

type keyDefault struct {
	ctx    keyContext
	chord  string
	action keyAction
}

func defaultKeyBindings() []keyDefault {
	var defaults []keyDefault
	add := func(ctx keyContext, action keyAction, chords ...string) {
		for _, chord := range chords {
			defaults = append(defaults, keyDefault{ctx: ctx, chord: chord, action: action})
		}
	}
	add(ctxGlobal, actInterrupt, "ctrl+c")
	add(ctxGlobal, actToggleTodos, "ctrl+t")
	add(ctxGlobal, actToggleTranscript, "ctrl+o")
	add(ctxGlobal, actToggleBrief, "ctrl+shift+b")
	add(ctxGlobal, actDiffFileUp, "ctrl+up", "alt+up")
	add(ctxGlobal, actDiffFileDown, "ctrl+down", "alt+down")
	add(ctxGlobal, actOpenArtifact, "ctrl+]")
	add(ctxGlobal, actHistorySearch, "ctrl+r")
	add(ctxChat, actSubmit, "enter")
	add(ctxChat, actExit, "ctrl+d")
	add(ctxChat, actQueueRedirect, "alt+enter")
	add(ctxChat, actNewline, "ctrl+j", "shift+enter")
	add(ctxChat, actCancel, "esc")
	add(ctxChat, actCycleMode, "shift+tab")
	add(ctxChat, actTabsNext, "tab")
	add(ctxChat, actHistoryPrevious, "up")
	add(ctxChat, actHistoryNext, "down")
	add(ctxChat, actImagePaste, "ctrl+v")
	add(ctxChat, actRemovePending, "ctrl+u")
	add(ctxChat, actStash, "ctrl+s")
	add(ctxChat, actClearInput, "ctrl+l")
	add(ctxChat, actChatSearch, "ctrl+f")
	add(ctxChat, actCloseTab, "ctrl+w")
	add(ctxChat, actOpenBoard, "ctrl+p")
	add(ctxChat, actExternalEditor, "ctrl+g", "ctrl+x ctrl+e")
	add(ctxChat, actKillAgents, "ctrl+x ctrl+k")
	add(ctxChat, actQueueSubmit, "ctrl+x enter")
	add(ctxChat, actSendNow, "ctrl+x ctrl+s", "alt+f12")
	add(ctxChat, actCopyCode, "ctrl+x c")
	add(ctxChat, actCopyReply, "alt+y")
	add(ctxChat, actToggleCodeFold, "alt+c")
	add(ctxChat, actToggleToolExpand, "alt+e")
	add(ctxChat, actToggleTurnFold, "ctrl+x f")
	add(ctxChat, actOpenOrFold, "ctrl+x o")
	add(ctxChat, actNextLink, "ctrl+x l")
	add(ctxChat, actNextTurn, "ctrl+x j")
	add(ctxChat, actPreviousTurn, "ctrl+x k")
	add(ctxChat, actToggleMouse, "ctrl+x m")
	add(ctxChat, actOpenDiff, "ctrl+x d")
	add(ctxChat, actOpenSessions, "ctrl+x s")
	add(ctxChat, actOpenTheme, "ctrl+x t")
	add(ctxChat, actToggleSidebar, "ctrl+x v")
	add(ctxChat, actHelpToggle, "?")
	add(ctxChat, actNotifyCycle, "n")
	add(ctxChat, "chat:undo", "ctrl+_", "ctrl+-", "ctrl+shift+-", "ctrl+shift+_")
	add(ctxChat, "chat:modelPicker", "alt+p")
	add(ctxChat, "chat:fastMode", "alt+o")
	add(ctxChat, "chat:defaultToNewerModel", "ctrl+y")
	add(ctxChat, "chat:thinkingToggle", "alt+t")
	add(ctxChat, "chat:workflowKeywordToggle", "alt+w")
	add(ctxChat, "chat:clearScreen", "cmd+k")
	add(ctxChat, "abovePrompt:toggle", "ctrl+x ctrl+a")
	add(ctxChat, "abovePrompt:focus", "ctrl+x tab")
	add(ctxChat, "task:background", "ctrl+b", "ctrl+x ctrl+b")
	add(ctxAutocomplete, actAutoAccept, "tab")
	add(ctxAutocomplete, actAutoDismiss, "esc")
	add(ctxAutocomplete, actAutoPrevious, "up", "ctrl+p")
	add(ctxAutocomplete, actAutoNext, "down", "ctrl+n")
	add(ctxAutocomplete, actSelectPageUp, "pgup")
	add(ctxAutocomplete, actSelectPageDown, "pgdown")
	add(ctxAutocomplete, actSelectFirst, "home")
	add(ctxAutocomplete, actSelectLast, "end")
	add(ctxMention, actAutoPrevious, "up")
	add(ctxMention, actAutoNext, "down")
	add(ctxMention, actAutoAccept, "tab")
	add(ctxPane, actPaneNext, "tab", "right")
	add(ctxPane, actPanePrevious, "shift+tab", "left")
	add(ctxPane, actPaneZoom, "ctrl+w")
	add(ctxPane, actPaneSend, "enter")
	add(ctxPane, actPaneClose, "esc", "ctrl+p", "ctrl+x x")
	add(ctxPane, "pane:grow", "ctrl+x left", "ctrl+x up")
	add(ctxPane, "pane:shrink", "ctrl+x right", "ctrl+x down")
	add(ctxQueue, actQueuePrevious, "up")
	add(ctxQueue, actQueueNext, "down")
	add(ctxQueue, actQueueMoveUp, "shift+up")
	add(ctxQueue, actQueueMoveDown, "shift+down")
	add(ctxQueue, actQueueRemove, "delete")
	add(ctxQueue, actQueueLeave, "esc")
	add(ctxApproval, actSelectPrevious, "up")
	add(ctxApproval, actSelectNext, "down")
	add(ctxApproval, actSelectAccept, "enter")
	add(ctxApprovalReason, actSelectCancel, "esc")
	add(ctxApprovalReason, actSelectAccept, "enter")
	add(ctxBoard, actSelectPrevious, "up")
	add(ctxBoard, actSelectNext, "down", "ctrl+n")
	add(ctxBoard, actSelectAccept, "enter")
	add(ctxBoard, actSelectCancel, "esc")
	add(ctxBoard, actBoardPanes, "ctrl+p")
	addSelectionNav(add, ctxSessions)
	add(ctxSessions, actSelectAccept, "enter")
	add(ctxSessions, actSelectCancel, "esc")
	add(ctxSearch, actSearchClose, "esc", "ctrl+f")
	add(ctxSearch, actSearchNext, "enter", "ctrl+n")
	add(ctxSearch, actSearchPrevious, "ctrl+p")
	add(ctxContext, actContextCompact, "c")
	add(ctxContext, actContextSync, "k")
	add(ctxContext, actNotifyCycle, "n")
	add(ctxContext, actContextClose, "z", "esc")
	add(ctxStatusline, actStatuslineClose, "esc")
	add(ctxStatusline, actStatuslineSource, "s")
	add(ctxStatusline, actStatuslineUp, "shift+up")
	add(ctxStatusline, actStatuslineDown, "shift+down")
	add(ctxStatusline, actStatuslineToggle, "space")
	addSelectionNav(add, ctxStatusline)
	add(ctxPlan, actSelectPrevious, "up")
	add(ctxPlan, actSelectNext, "down")
	add(ctxPlan, actSelectAccept, "enter")
	add(ctxPlan, actPlanSkip, "esc")
	add(ctxPlan, actCycleMode, "shift+tab")
	add(ctxSkills, actSelectCancel, "esc")
	add(ctxSkills, actSelectAccept, "enter")
	addSelectionNav(add, ctxSkills)
	add(ctxResume, actSelectAccept, "enter")
	add(ctxResume, actSelectCancel, "esc")
	addSelectionNav(add, ctxResume)
	add(ctxClose, actSessionClose, "y")
	add(ctxClose, actExit, "esc")
	add(ctxPlugin, actSelectCancel, "esc")
	add(ctxPlugin, actSelectAccept, "enter")
	add(ctxPlugin, actPluginToggle, "space")
	add(ctxPlugin, actPluginInstall, "i")
	add(ctxPlugin, actPluginFavorite, "f")
	add(ctxPlugin, actPluginMarket, "ctrl+s")
	addSelectionNav(add, ctxPlugin)
	add(ctxThemePicker, actSelectAccept, "enter")
	add(ctxThemePicker, actSelectCancel, "esc")
	add(ctxThemePicker, actThemeEdit, "ctrl+e", "e")
	add(ctxThemePicker, actThemeSyntax, "ctrl+t")
	add(ctxThemePicker, actThemeInstall, "f")
	add(ctxThemePicker, actThemeGlyphs, "g")
	add(ctxThemePicker, actThemeBorders, "b")
	add(ctxThemePicker, actThemeDensity, "d")
	add(ctxThemePicker, actThemeBold, "w")
	add(ctxThemePicker, actThemeItalic, "i")
	add(ctxThemePicker, actThemeAuto, "a")
	addSelectionNav(add, ctxThemePicker)
	add(ctxThemeInstall, actThemeInstall, "f")
	add(ctxThemeInstall, actThemeCopyCommand, "c")
	add(ctxThemeInstall, actThemeCopyConfig, "y")
	add(ctxHistorySearch, actHistoryAccept, "enter", "tab")
	add(ctxHistorySearch, actHistoryCancel, "esc")
	add(ctxHistorySearch, actHistoryNextMatch, "ctrl+r")
	add(ctxHistorySearch, actHistoryCycleScope, "ctrl+s")
	add(ctxHistorySearch, actSelectPrevious, "up", "ctrl+p")
	add(ctxHistorySearch, actSelectNext, "down", "ctrl+n")
	add(ctxMessageSelector, actSelectAccept, "enter")
	add(ctxMessageSelector, actSelectCancel, "esc")
	add(ctxMessageSelector, actRewindFork, "f")
	add(ctxMessageSelector, actRewindUndo, "u")
	add(ctxMessageSelector, actConfirmYes, "y", "Y")
	add(ctxMessageSelector, actConfirmNo, "n", "N")
	addSelectionNav(add, ctxMessageSelector)
	add(ctxDiffPanel, actDiffDismiss, "esc", "ctrl+d")
	add(ctxDiffPanel, actDiffPrevFile, "up", "k")
	add(ctxDiffPanel, actDiffNextFile, "down", "j")
	add(ctxDiffPanel, actScrollPageUp, "pgup")
	add(ctxDiffPanel, actScrollPageDown, "pgdown")
	add(ctxDiffPanel, actDiffReviewed, "space")
	add(ctxDiffPanel, actDiffOpenFile, "enter")
	add(ctxDiffPanel, actDiffNote, "i")
	add(ctxDiffPanel, actDiffCommit, "c")
	add(ctxDiffPanel, actDiffPR, "p")
	add(ctxDiffPanel, actCycleDiffBase, "b", "ctrl+x b")
	add(ctxDiffDialog, actDiffDismiss, "esc")
	add(ctxDiffDialog, actDiffCommit, "enter")
	add(ctxDiffDialog, actDiffPR, "p")
	add(ctxDiffDialog, actDiffEdit, "e")
	add(ctxDiffNote, actDiffDismiss, "esc")
	add(ctxDiffNote, actDiffSendNote, "enter")
	add(ctxDiffMessage, actDiffStopEdit, "esc")
	add(ctxDiffMessage, actDiffCommit, "enter")
	add(ctxDiffMessage, actNewline, "ctrl+j")
	return defaults
}

func addSelectionNav(add func(keyContext, keyAction, ...string), ctx keyContext) {
	add(ctx, actSelectPrevious, "up", "ctrl+p")
	add(ctx, actSelectNext, "down", "ctrl+n")
	add(ctx, actSelectPageUp, "pgup")
	add(ctx, actSelectPageDown, "pgdown")
	add(ctx, actSelectFirst, "home")
	add(ctx, actSelectLast, "end")
}

func knownKeyContexts() []keyContext {
	return []keyContext{
		ctxGlobal, ctxChat, ctxAutocomplete, ctxHistorySearch, ctxMessageSelector,
		ctxThemePicker, ctxDiffPanel, ctxDiffDialog, ctxPane, ctxPlugin, ctxSelect,
		ctxScroll, ctxConfirmation, ctxTask, ctxFooter, ctxAbovePrompt, ctxAbovePromptIn,
		ctxAbovePromptSel, ctxTabs, ctxAttachments, ctxHelp, ctxTranscript, ctxSettings,
		ctxModelPicker, ctxEffortSlider, ctxAgents, ctxBoard, ctxSessions, ctxSearch,
		ctxDiagnostics, ctxContext, ctxStatusline, ctxPlan, ctxSkills, ctxResume, ctxClose,
		ctxQueue, ctxApproval, ctxApprovalReason, ctxMention, ctxThemeInstall, ctxDiffNote,
		ctxDiffMessage,
	}
}

func knownKeyActions() []keyAction {
	return []keyAction{
		actInterrupt, actExit, actToggleTodos, actToggleTranscript, actToggleBrief,
		actDiffFileUp, actDiffFileDown, actCycleDiffBase, actOpenArtifact, actOpenBoard,
		actOpenPanes, actOpenDiff, actOpenSessions, actOpenTheme, actToggleMouse,
		actToggleSidebar, actCloseTab, actHistorySearch, actHistoryPrevious, actHistoryNext,
		actTabsNext, actCancel, actSubmit, actKillAgents, actCycleMode,
		actQueueSubmit, actSendNow, actNewline, actExternalEditor, actStash, actImagePaste,
		actClearInput, actQueueRedirect, actChatSearch, actCopyCode, actCopyReply,
		actToggleCodeFold, actToggleToolExpand, actToggleTurnFold, actOpenOrFold, actNextLink,
		actNextTurn, actPreviousTurn, actRemovePending, actHelpToggle, actNotifyCycle,
		actSelectNext, actSelectPrevious, actSelectPageUp, actSelectPageDown, actSelectFirst,
		actSelectLast, actSelectAccept, actSelectCancel, actAutoAccept, actAutoDismiss,
		actAutoPrevious, actAutoNext, actPaneNext, actPanePrevious, actPaneZoom, actPaneSend,
		actPaneClose, actDiffDismiss, actDiffPrevFile, actDiffNextFile,
		actDiffReviewed, actDiffOpenFile, actDiffNote, actDiffCommit, actDiffPR, actDiffEdit,
		actDiffSendNote, actDiffStopEdit, actScrollPageUp, actScrollPageDown, actThemeEdit,
		actThemeSyntax, actThemeInstall, actThemeGlyphs, actThemeBorders, actThemeDensity,
		actThemeBold, actThemeItalic, actThemeAuto, actThemeCopyCommand, actThemeCopyConfig,
		actSearchClose, actSearchNext, actSearchPrevious, actBoardPanes, actContextCompact,
		actContextSync, actContextClose, actStatuslineClose, actStatuslineSource,
		actStatuslineUp, actStatuslineDown, actStatuslineToggle, actPlanSkip, actSessionClose,
		actQueuePrevious, actQueueNext, actQueueMoveUp, actQueueMoveDown, actQueueRemove,
		actQueueLeave, actRewindFork, actRewindUndo, actConfirmYes, actConfirmNo,
		actPluginToggle, actPluginInstall, actPluginFavorite, actPluginMarket,
		actHistoryCycleScope, actHistoryAccept, actHistoryCancel, actHistoryNextMatch,
		actApprovalDeny,
		"chat:undo", "chat:modelPicker", "chat:fastMode", "chat:defaultToNewerModel",
		"chat:thinkingToggle", "chat:workflowKeywordToggle", "chat:clearScreen",
		"abovePrompt:toggle", "abovePrompt:focus", "task:background", "pane:grow", "pane:shrink",
		"voice:pushToTalk", "app:toggleReplTab", "app:toggleDiffNoiseFilter",
		"app:toggleDiffPreSession", "app:toggleTerminal", "app:redraw",
		"permission:toggleDebug", "help:dismiss", "tabs:next", "transcript:exit",
		"scroll:lineUp", "scroll:lineDown", "scroll:top", "scroll:bottom",
		"scroll:halfPageUp", "scroll:halfPageDown", "scroll:fullPageUp", "scroll:fullPageDown",
		"selection:copy", "selection:extendLeft", "selection:extendRight", "selection:extendUp",
		"selection:extendDown", "selection:extendLineStart", "selection:extendLineEnd",
		"confirm:previous", "confirm:next", "confirm:nextField", "confirm:previousField",
		"confirm:cycleMode", "confirm:toggle", "historySearch:execute",
		"plugin:toggle", "settings:search", "settings:retry",
		"agents:switchView", "agents:togglePin", "agents:find", "agents:nextGroup",
		"agents:previousGroup", "agents:rename",
		"modelPicker:decreaseEffort", "modelPicker:increaseEffort", "modelPicker:thisSessionOnly",
		"effortSlider:decreaseEffort", "effortSlider:increaseEffort", "effortSlider:toggleUltracode",
		"attachments:next", "attachments:previous", "attachments:remove", "attachments:exit",
		"footer:up", "footer:down", "footer:next", "footer:previous", "footer:openSelected",
		"footer:clearSelection", "footer:close", "abovePrompt:next", "abovePrompt:previous",
		"abovePrompt:press", "abovePrompt:leave", "abovePrompt:highlightNext",
		"abovePrompt:highlightPrevious", "pane:scrollUp", "pane:scrollDown", "pane:pageUp",
		"pane:pageDown", "pane:top", "pane:bottom", "diff:previousSource", "diff:nextSource",
		"diff:back", "tabs:previous", "theme:toggleSyntaxHighlighting",
	}
}

func isKnownKeyAction(action keyAction) bool {
	return slices.Contains(knownKeyActions(), action)
}
