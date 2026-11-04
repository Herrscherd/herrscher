package tui

import (
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

func TestEveryDefaultBindingHasAHandlerOrIsListedWithoutEffect(t *testing.T) {
	tables, err := newDispatchTables()
	if err != nil {
		t.Fatal(err)
	}
	handled := make(map[keyAction]bool)
	for _, action := range boundActions(tables) {
		handled[action] = true
	}
	silent := make(map[keyAction]bool)
	for _, action := range defaultsWithoutEffect() {
		silent[action] = true
		if handled[action] {
			t.Errorf("%s is listed without effect but has a handler", action)
		}
	}
	var unhandled []string
	for _, chords := range defaultKeyTable().chords {
		for _, action := range chords {
			if !handled[action] && !silent[action] {
				unhandled = append(unhandled, string(action))
			}
		}
	}
	sort.Strings(unhandled)
	if len(unhandled) > 0 {
		t.Fatalf("bound actions with no handler and not listed without effect: %s", strings.Join(unhandled, ", "))
	}
}
