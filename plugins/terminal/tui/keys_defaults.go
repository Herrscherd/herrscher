package tui

import "slices"

const (
	ctxGlobal          keyContext = "Global"
	ctxChat            keyContext = "Chat"
	ctxToast           keyContext = "Toast"
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
	ctxHealth          keyContext = "Health"
	ctxUsage           keyContext = "Usage"
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
	actInterrupt            keyAction = "app:interrupt"
	actExit                 keyAction = "app:exit"
	actToggleTodos          keyAction = "app:toggleTodos"
	actToggleTranscript     keyAction = "app:toggleTranscript"
	actToggleBrief          keyAction = "app:toggleBrief"
	actDiffFileUp           keyAction = "app:diffFileListUp"
	actDiffFileDown         keyAction = "app:diffFileListDown"
	actCycleDiffBase        keyAction = "app:cycleDiffBase"
	actOpenArtifact         keyAction = "app:openArtifact"
	actOpenBoard            keyAction = "app:openBoard"
	actOpenDiff             keyAction = "app:openDiff"
	actOpenSessions         keyAction = "app:openSessions"
	actOpenTheme            keyAction = "app:openTheme"
	actToggleMouse          keyAction = "app:toggleMouse"
	actToggleSidebar        keyAction = "app:toggleSidebar"
	actCloseTab             keyAction = "app:closeTab"
	actHistorySearch        keyAction = "history:search"
	actHistoryPrevious      keyAction = "history:previous"
	actHistoryNext          keyAction = "history:next"
	actTabsNext             keyAction = "tabs:next"
	actCancel               keyAction = "chat:cancel"
	actSubmit               keyAction = "chat:submit"
	actKillAgents           keyAction = "chat:killAgents"
	actCycleMode            keyAction = "chat:cycleMode"
	actQueueSubmit          keyAction = "chat:queueSubmit"
	actSendNow              keyAction = "chat:sendNow"
	actNewline              keyAction = "chat:newline"
	actExternalEditor       keyAction = "chat:externalEditor"
	actStash                keyAction = "chat:stash"
	actImagePaste           keyAction = "chat:imagePaste"
	actClearInput           keyAction = "chat:clearInput"
	actQueueRedirect        keyAction = "chat:queueRedirect"
	actChatSearch           keyAction = "chat:search"
	actCopyCode             keyAction = "chat:copyCode"
	actCopyReply            keyAction = "chat:copyReply"
	actToggleCodeFold       keyAction = "chat:toggleCodeFold"
	actToggleToolExpand     keyAction = "chat:toggleToolExpand"
	actToggleTurnFold       keyAction = "chat:toggleTurnFold"
	actOpenOrFold           keyAction = "chat:openOrFold"
	actNextLink             keyAction = "chat:nextLink"
	actNextTurn             keyAction = "chat:nextTurn"
	actPreviousTurn         keyAction = "chat:previousTurn"
	actRemovePending        keyAction = "chat:removePending"
	actHelpToggle           keyAction = "chat:helpToggle"
	actNotifyCycle          keyAction = "chat:notifyCycle"
	actSelectNext           keyAction = "select:next"
	actSelectPrevious       keyAction = "select:previous"
	actSelectPageUp         keyAction = "select:pageUp"
	actSelectPageDown       keyAction = "select:pageDown"
	actSelectFirst          keyAction = "select:first"
	actSelectLast           keyAction = "select:last"
	actSelectAccept         keyAction = "select:accept"
	actSelectCancel         keyAction = "select:cancel"
	actDiagnosticsClose     keyAction = "diagnostics:close"
	actPlanChoose           keyAction = "plan:choose"
	actPaneSideNext         keyAction = "pane:sideNext"
	actPaneSidePrevious     keyAction = "pane:sidePrevious"
	actApprovalAllow        keyAction = "approval:allow"
	actApprovalAllowSession keyAction = "approval:allowSession"
	actApprovalAllowAlways  keyAction = "approval:allowAlways"
	actApprovalAccelerate   keyAction = "approval:accelerate"
	actToastJump            keyAction = "toast:jump"
	actChatUndo             keyAction = "chat:undo"
	actChatModelPicker      keyAction = "chat:modelPicker"
	actChatFastMode         keyAction = "chat:fastMode"
	actChatNewerModel       keyAction = "chat:defaultToNewerModel"
	actChatThinking         keyAction = "chat:thinkingToggle"
	actChatWorkflowKeyword  keyAction = "chat:workflowKeywordToggle"
	actChatClearScreen      keyAction = "chat:clearScreen"
	actAbovePromptToggle    keyAction = "abovePrompt:toggle"
	actAbovePromptFocus     keyAction = "abovePrompt:focus"
	actTaskBackground       keyAction = "task:background"
	actAutoAccept           keyAction = "autocomplete:accept"
	actAutoDismiss          keyAction = "autocomplete:dismiss"
	actPaneNext             keyAction = "pane:next"
	actPanePrevious         keyAction = "pane:previous"
	actPaneZoom             keyAction = "pane:zoom"
	actPaneSend             keyAction = "pane:send"
	actPaneClose            keyAction = "pane:close"
	actDiffDismiss          keyAction = "diff:dismiss"
	actDiffPrevFile         keyAction = "diff:previousFile"
	actDiffNextFile         keyAction = "diff:nextFile"
	actDiffReviewed         keyAction = "diff:toggleReviewed"
	actDiffOpenFile         keyAction = "diff:openFile"
	actDiffNote             keyAction = "diff:note"
	actDiffCommit           keyAction = "diff:commit"
	actDiffPR               keyAction = "diff:pr"
	actDiffEdit             keyAction = "diff:edit"
	actDiffSendNote         keyAction = "diff:sendNote"
	actDiffStopEdit         keyAction = "diff:stopEdit"
	actScrollPageUp         keyAction = "scroll:pageUp"
	actScrollPageDown       keyAction = "scroll:pageDown"
	actThemeEdit            keyAction = "theme:editCustom"
	actThemeInstall         keyAction = "theme:install"
	actThemeGlyphs          keyAction = "theme:glyphs"
	actThemeBorders         keyAction = "theme:borders"
	actThemeDensity         keyAction = "theme:density"
	actThemeBold            keyAction = "theme:bold"
	actThemeItalic          keyAction = "theme:italic"
	actThemeAuto            keyAction = "theme:auto"
	actThemeCopyCommand     keyAction = "theme:copyCommand"
	actThemeCopyConfig      keyAction = "theme:copyConfig"
	actSearchClose          keyAction = "search:close"
	actSearchNext           keyAction = "search:next"
	actSearchPrevious       keyAction = "search:previous"
	actBoardPanes           keyAction = "board:panes"
	actContextCompact       keyAction = "context:compact"
	actContextSync          keyAction = "context:syncTabs"
	actContextClose         keyAction = "context:close"
	actHealthRefresh        keyAction = "health:refresh"
	actHealthDeep           keyAction = "health:deep"
	actHealthDetail         keyAction = "health:detail"
	actHealthClose          keyAction = "health:close"
	actUsageRefresh         keyAction = "usage:refresh"
	actUsageTab             keyAction = "usage:tab"
	actUsageRangePrevious   keyAction = "usage:range-previous"
	actUsageRangeNext       keyAction = "usage:range-next"
	actUsageAccount         keyAction = "usage:account"
	actUsageVendor          keyAction = "usage:vendor"
	actUsageClose           keyAction = "usage:close"
	actStatuslineClose      keyAction = "statusline:close"
	actStatuslineSource     keyAction = "statusline:switchSource"
	actStatuslineUp         keyAction = "statusline:fieldUp"
	actStatuslineDown       keyAction = "statusline:fieldDown"
	actStatuslineToggle     keyAction = "statusline:toggleField"
	actPlanSkip             keyAction = "plan:skip"
	actSessionClose         keyAction = "session:confirmClose"
	actQueuePrevious        keyAction = "queue:previous"
	actQueueNext            keyAction = "queue:next"
	actQueueMoveUp          keyAction = "queue:moveUp"
	actQueueMoveDown        keyAction = "queue:moveDown"
	actQueueRemove          keyAction = "queue:remove"
	actQueueLeave           keyAction = "queue:leave"
	actRewindFork           keyAction = "rewind:fork"
	actRewindUndo           keyAction = "rewind:undo"
	actConfirmYes           keyAction = "confirm:yes"
	actConfirmNo            keyAction = "confirm:no"
	actHistoryAccept        keyAction = "historySearch:accept"
	actHistoryCancel        keyAction = "historySearch:cancel"
	actHistoryNextMatch     keyAction = "historySearch:next"
	actApprovalDeny         keyAction = "approval:reason"
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
	for _, ctx := range modalKeyContexts() {
		add(ctx, actInterrupt, "ctrl+c")
	}
	add(ctxGlobal, actToggleTodos, "ctrl+t")
	add(ctxGlobal, actToggleTranscript, "ctrl+o")
	add(ctxGlobal, actToggleBrief, "ctrl+shift+b")
	add(ctxGlobal, actDiffFileUp, "ctrl+up", "alt+up")
	add(ctxGlobal, actDiffFileDown, "ctrl+down", "alt+down")
	add(ctxGlobal, actOpenArtifact, "ctrl+]")
	add(ctxGlobal, actHistorySearch, "ctrl+r")
	add(ctxToast, actToastJump, "ctrl+p")
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
	add(ctxChat, actChatUndo, "ctrl+_", "ctrl+-", "ctrl+shift+-", "ctrl+shift+_")
	add(ctxChat, actChatModelPicker, "alt+p")
	add(ctxChat, actChatFastMode, "alt+o")
	add(ctxChat, actChatNewerModel, "ctrl+y")
	add(ctxChat, actChatThinking, "alt+t")
	add(ctxChat, actChatWorkflowKeyword, "alt+w")
	add(ctxChat, actChatClearScreen, "cmd+k")
	add(ctxChat, actAbovePromptToggle, "ctrl+x ctrl+a")
	add(ctxChat, actAbovePromptFocus, "ctrl+x tab")
	add(ctxChat, actTaskBackground, "ctrl+b", "ctrl+x ctrl+b")
	add(ctxAutocomplete, actAutoAccept, "tab")
	add(ctxAutocomplete, actAutoDismiss, "esc")
	add(ctxAutocomplete, actSelectPrevious, "up", "ctrl+p")
	add(ctxAutocomplete, actSelectNext, "down", "ctrl+n")
	add(ctxAutocomplete, actSelectAccept, "enter")
	add(ctxAutocomplete, actSelectPageUp, "pgup")
	add(ctxAutocomplete, actSelectPageDown, "pgdown")
	add(ctxAutocomplete, actSelectFirst, "home")
	add(ctxAutocomplete, actSelectLast, "end")
	add(ctxMention, actSelectPrevious, "up")
	add(ctxMention, actSelectNext, "down")
	add(ctxMention, actAutoAccept, "tab")
	add(ctxPane, actPaneNext, "tab")
	add(ctxPane, actPanePrevious, "shift+tab")
	add(ctxPane, actPaneSideNext, "right")
	add(ctxPane, actPaneSidePrevious, "left")
	add(ctxPane, actPaneZoom, "ctrl+w")
	add(ctxPane, actPaneSend, "enter")
	add(ctxPane, actPaneClose, "esc", "ctrl+p", "ctrl+x x")
	add(ctxQueue, actQueuePrevious, "up")
	add(ctxQueue, actQueueNext, "down")
	add(ctxQueue, actQueueMoveUp, "shift+up")
	add(ctxQueue, actQueueMoveDown, "shift+down")
	add(ctxQueue, actQueueRemove, "delete")
	add(ctxQueue, actQueueLeave, "esc")
	add(ctxApproval, actSelectPrevious, "up")
	add(ctxApproval, actSelectNext, "down")
	add(ctxApproval, actSelectAccept, "enter")
	add(ctxApproval, actApprovalAllow, "o")
	add(ctxApproval, actApprovalAllowSession, "s")
	add(ctxApproval, actApprovalAllowAlways, "t")
	add(ctxApproval, actApprovalDeny, "r")
	add(ctxApproval, actApprovalAccelerate, "x")
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
	add(ctxDiagnostics, actDiagnosticsClose, "esc")
	add(ctxSearch, actSearchClose, "esc", "ctrl+f")
	add(ctxSearch, actSearchNext, "enter", "ctrl+n")
	add(ctxSearch, actSearchPrevious, "ctrl+p")
	add(ctxContext, actContextCompact, "c")
	add(ctxContext, actContextSync, "k")
	add(ctxContext, actNotifyCycle, "n")
	add(ctxContext, actContextClose, "z", "esc")
	add(ctxHealth, actHealthRefresh, "r")
	add(ctxHealth, actHealthDeep, "d")
	add(ctxHealth, actHealthDetail, "enter")
	add(ctxHealth, actHealthClose, "esc")
	add(ctxUsage, actUsageRefresh, "r")
	add(ctxUsage, actUsageTab, "tab")
	add(ctxUsage, actUsageRangePrevious, "left")
	add(ctxUsage, actUsageRangeNext, "right")
	add(ctxUsage, actUsageAccount, "a")
	add(ctxUsage, actUsageVendor, "f")
	add(ctxUsage, actUsageClose, "esc")
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
	for _, digit := range "123456789" {
		add(ctxPlan, actPlanChoose, string(digit))
	}
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
	addSelectionNav(add, ctxPlugin)
	add(ctxThemePicker, actSelectAccept, "enter")
	add(ctxThemePicker, actSelectCancel, "esc")
	add(ctxThemePicker, actThemeEdit, "ctrl+e", "e")
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
	addSelectionNav(add, ctxThemeInstall)
	add(ctxThemeInstall, actSelectAccept, "enter")
	add(ctxThemeInstall, actSelectCancel, "esc")
	add(ctxHistorySearch, actHistoryAccept, "enter", "tab")
	add(ctxHistorySearch, actHistoryCancel, "esc")
	add(ctxHistorySearch, actHistoryNextMatch, "ctrl+r")
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
	add(ctxDiffPanel, actDiffPrevFile, "ctrl+up", "alt+up")
	add(ctxDiffPanel, actDiffNextFile, "ctrl+down", "alt+down")
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

func modalKeyContexts() []keyContext {
	return []keyContext{
		ctxAutocomplete, ctxMention, ctxPane, ctxQueue, ctxApproval, ctxApprovalReason, ctxBoard,
		ctxSessions, ctxSearch, ctxContext, ctxStatusline, ctxPlan, ctxSkills, ctxResume, ctxClose,
		ctxPlugin, ctxThemePicker, ctxThemeInstall, ctxHistorySearch, ctxMessageSelector,
		ctxDiffPanel, ctxDiffDialog, ctxDiffMessage, ctxDiagnostics,
	}
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
		ctxGlobal, ctxChat, ctxToast, ctxAutocomplete, ctxHistorySearch, ctxMessageSelector,
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
		actInterrupt, actExit, actToggleTodos, actToggleTranscript, actToggleBrief, actDiffFileUp,
		actDiffFileDown, actCycleDiffBase, actOpenArtifact, actOpenBoard, actOpenDiff,
		actOpenSessions, actOpenTheme, actToggleMouse, actToggleSidebar, actCloseTab,
		actHistorySearch, actHistoryPrevious, actHistoryNext, actTabsNext, actCancel, actSubmit,
		actKillAgents, actCycleMode, actQueueSubmit, actSendNow, actNewline, actExternalEditor,
		actStash, actImagePaste, actClearInput, actQueueRedirect, actChatSearch, actCopyCode,
		actCopyReply, actToggleCodeFold, actToggleToolExpand, actToggleTurnFold, actOpenOrFold,
		actNextLink, actNextTurn, actPreviousTurn, actRemovePending, actHelpToggle, actNotifyCycle,
		actSelectNext, actSelectPrevious, actSelectPageUp, actSelectPageDown, actSelectFirst,
		actSelectLast, actSelectAccept, actSelectCancel, actAutoAccept, actAutoDismiss, actPaneNext,
		actPanePrevious, actPaneZoom, actPaneSend, actPaneClose, actDiffDismiss, actDiffPrevFile,
		actDiffNextFile, actDiffReviewed, actDiffOpenFile, actDiffNote, actDiffCommit, actDiffPR,
		actDiffEdit, actDiffSendNote, actDiffStopEdit, actScrollPageUp, actScrollPageDown,
		actThemeEdit, actThemeInstall, actThemeGlyphs, actThemeBorders, actThemeDensity,
		actThemeBold, actThemeItalic, actThemeAuto, actThemeCopyCommand, actThemeCopyConfig,
		actSearchClose, actSearchNext, actSearchPrevious, actBoardPanes, actContextCompact,
		actContextSync, actContextClose, actHealthRefresh, actHealthDeep, actHealthDetail, actHealthClose, actUsageRefresh, actUsageTab, actUsageRangePrevious, actUsageRangeNext, actUsageAccount, actUsageVendor, actUsageClose, actStatuslineClose, actStatuslineSource, actStatuslineUp,
		actStatuslineDown, actStatuslineToggle, actPlanSkip, actSessionClose, actQueuePrevious,
		actQueueNext, actQueueMoveUp, actQueueMoveDown, actQueueRemove, actQueueLeave, actRewindFork,
		actRewindUndo, actConfirmYes, actConfirmNo, actHistoryAccept, actHistoryCancel,
		actHistoryNextMatch, actApprovalDeny, actDiagnosticsClose, actPlanChoose, actPaneSideNext,
		actPaneSidePrevious, actApprovalAllow, actApprovalAllowSession, actApprovalAllowAlways,
		actApprovalAccelerate, actToastJump, actChatUndo, actChatModelPicker, actChatFastMode,
		actChatNewerModel, actChatThinking, actChatWorkflowKeyword, actChatClearScreen,
		actAbovePromptToggle, actAbovePromptFocus, actTaskBackground,
	}
}

func isKnownKeyAction(action keyAction) bool {
	return slices.Contains(knownKeyActions(), action) || slices.Contains(claudeReferenceActions(), action)
}
