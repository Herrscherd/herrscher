package tui

import (
	"slices"
	"sort"
	"strings"
	"testing"
)

func defaultsWithoutEffect() []keyAction {
	return []keyAction{
		actToggleTodos, actToggleBrief, actDiffFileUp, actDiffFileDown, actOpenArtifact,
		actChatClearScreen, actChatModelPicker, actChatFastMode, actChatNewerModel,
		actChatThinking, actChatWorkflowKeyword, actAbovePromptToggle, actAbovePromptFocus,
		actTaskBackground,
	}
}

func keysOf[Run any](table map[keyAction]Run) []keyAction {
	keys := make([]keyAction, 0, len(table))
	for action := range table {
		keys = append(keys, action)
	}
	return keys
}

func boundActions(tables *dispatchTables) []keyAction {
	var actions []keyAction
	actions = append(actions, keysOf(tables.approvalReason)...)
	actions = append(actions, keysOf(tables.approval)...)
	actions = append(actions, keysOf(tables.mention)...)
	actions = append(actions, keysOf(tables.palette)...)
	actions = append(actions, keysOf(tables.resume)...)
	actions = append(actions, keysOf(tables.skills)...)
	actions = append(actions, keysOf(tables.diag)...)
	actions = append(actions, keysOf(tables.search)...)
	actions = append(actions, keysOf(tables.switchOverlay)...)
	actions = append(actions, keysOf(tables.pendingClose)...)
	actions = append(actions, keysOf(tables.themeLook)...)
	actions = append(actions, keysOf(tables.themeScreen)...)
	actions = append(actions, keysOf(tables.statuslineField)...)
	actions = append(actions, keysOf(tables.statusline)...)
	actions = append(actions, keysOf(tables.rewindSure)...)
	actions = append(actions, keysOf(tables.rewindOptions)...)
	actions = append(actions, keysOf(tables.rewindList)...)
	actions = append(actions, keysOf(tables.queue)...)
	actions = append(actions, keysOf(tables.plugins)...)
	actions = append(actions, keysOf(tables.panes)...)
	actions = append(actions, keysOf(tables.board)...)
	actions = append(actions, keysOf(tables.plan)...)
	actions = append(actions, keysOf(tables.context)...)
	actions = append(actions, keysOf(tables.nav)...)
	actions = append(actions, keysOf(tables.diffMessage)...)
	actions = append(actions, keysOf(tables.diffDialog)...)
	actions = append(actions, keysOf(tables.diffNote)...)
	actions = append(actions, keysOf(tables.diffPanel)...)
	actions = append(actions, keysOf(tables.chat)...)
	actions = append(actions, keysOf(tables.histSearch)...)
	actions = append(actions, keysOf(tables.toast)...)
	return actions
}

func TestDispatchTablesRefuseDuplicateBindings(t *testing.T) {
	if _, err := newDispatchTables(); err != nil {
		t.Fatalf("a dispatch table binds one action twice: %v", err)
	}
	table, err := dispatchOf(bind(actInterrupt, keyRun(nil)), bind(actInterrupt, keyRun(nil)))
	if err == nil || len(table) != 1 {
		t.Fatalf("a duplicate must be reported, got %v with %d entries", err, len(table))
	}
}

func handlersByContext(tables *dispatchTables) map[keyContext][]keyAction {
	list := keysOf(tables.nav)
	handlers := make(map[keyContext][]keyAction)
	handlers[ctxGlobal] = boundActions(tables)
	handlers[ctxChat] = keysOf(tables.chat)
	handlers[ctxToast] = keysOf(tables.toast)
	handlers[ctxAutocomplete] = append(append(keysOf(tables.palette), keysOf(tables.mention)...), list...)
	handlers[ctxSessions] = append(keysOf(tables.switchOverlay), list...)
	handlers[ctxHistorySearch] = keysOf(tables.histSearch)
	handlers[ctxMessageSelector] = append(append(keysOf(tables.rewindList), keysOf(tables.rewindOptions)...), append(keysOf(tables.rewindSure), list...)...)
	handlers[ctxThemePicker] = append(append(keysOf(tables.themeLook), keysOf(tables.themeScreen)...), list...)
	handlers[ctxThemeInstall] = append(append(keysOf(tables.themeScreen), keysOf(tables.themeLook)...), list...)
	handlers[ctxDiffPanel] = keysOf(tables.diffPanel)
	handlers[ctxDiffDialog] = keysOf(tables.diffDialog)
	handlers[ctxDiffNote] = keysOf(tables.diffNote)
	handlers[ctxDiffMessage] = keysOf(tables.diffMessage)
	handlers[ctxPane] = keysOf(tables.panes)
	handlers[ctxPlugin] = append(keysOf(tables.plugins), list...)
	handlers[ctxBoard] = keysOf(tables.board)
	handlers[ctxSearch] = keysOf(tables.search)
	handlers[ctxDiagnostics] = keysOf(tables.diag)
	handlers[ctxContext] = keysOf(tables.context)
	handlers[ctxStatusline] = append(append(keysOf(tables.statusline), keysOf(tables.statuslineField)...), list...)
	handlers[ctxPlan] = keysOf(tables.plan)
	handlers[ctxSkills] = append(keysOf(tables.skills), list...)
	handlers[ctxResume] = append(keysOf(tables.resume), list...)
	handlers[ctxClose] = keysOf(tables.pendingClose)
	handlers[ctxQueue] = keysOf(tables.queue)
	handlers[ctxApproval] = keysOf(tables.approval)
	handlers[ctxApprovalReason] = keysOf(tables.approvalReason)
	handlers[ctxMention] = keysOf(tables.mention)
	return handlers
}

func chatFallthroughContexts() []keyContext {
	return []keyContext{ctxApproval, ctxApprovalReason, ctxMention, ctxAutocomplete, ctxQueue}
}

func missingContextHandlers(tables *dispatchTables) []string {
	handlers := handlersByContext(tables)
	silent := make(map[keyAction]bool)
	for _, action := range defaultsWithoutEffect() {
		silent[action] = true
	}
	chatHandled := make(map[keyAction]bool)
	for _, action := range keysOf(tables.chat) {
		chatHandled[action] = true
	}
	var missing []string
	for context, chords := range defaultKeyTable().chords {
		handled := make(map[keyAction]bool)
		for _, action := range handlers[context] {
			handled[action] = true
		}
		for _, action := range chords {
			if slices.Contains(chatFallthroughContexts(), context) && chatHandled[action] {
				continue
			}
			if !handled[action] && !silent[action] {
				missing = append(missing, string(context)+" "+string(action))
			}
		}
	}
	sort.Strings(missing)
	return missing
}

func TestEveryBoundActionHasAHandlerInItsContext(t *testing.T) {
	tables, err := newDispatchTables()
	if err != nil {
		t.Fatal(err)
	}
	if missing := missingContextHandlers(tables); len(missing) > 0 {
		t.Fatalf("bound actions with no handler in their context: %s", strings.Join(missing, ", "))
	}
}

func TestSilentActionsHaveNoHandler(t *testing.T) {
	tables, err := newDispatchTables()
	if err != nil {
		t.Fatal(err)
	}
	handled := make(map[keyAction]bool)
	for _, action := range boundActions(tables) {
		handled[action] = true
	}
	for _, action := range defaultsWithoutEffect() {
		if handled[action] {
			t.Errorf("%s is listed without effect but has a handler", action)
		}
	}
}
