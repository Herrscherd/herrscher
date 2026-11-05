package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestHelpGivesEachKeyOneMeaning(t *testing.T) {
	help := ansi.Strip(newTestModel().helpView())
	meaning := make(map[string]string)
	for entry := range strings.SplitSeq(help, " · ") {
		key, label, found := strings.Cut(strings.TrimSpace(entry), " ")
		if !found {
			t.Fatalf("help entry %q has no label", entry)
		}
		if previous, seen := meaning[key]; seen {
			t.Fatalf("%s means both %q and %q", key, previous, label)
		}
		meaning[key] = label
	}
}

func pressChord(m *model, chord string) {
	for _, name := range strings.Fields(chord) {
		m.Update(chordKey(name))
	}
}

func chordKey(name string) tea.KeyMsg {
	if alt, ok := strings.CutPrefix(name, "alt+"); ok {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(alt), Alt: true}
	}
	return keyFor(name)
}

func loadForTest(t *testing.T, herrscher, claude string, section keysSection) keyLoad {
	t.Helper()
	dir := t.TempDir()
	herrscherPath := filepath.Join(dir, keybindingsFile)
	claudePath := filepath.Join(dir, "claude-keybindings.json")
	if herrscher != "" {
		if err := os.WriteFile(herrscherPath, []byte(herrscher), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if claude != "" {
		if err := os.WriteFile(claudePath, []byte(claude), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return loadKeyBindings(claudePath, herrscherPath, section)
}

func actionOf(t *testing.T, table *keyTable, chord string, stack ...keyContext) keyAction {
	t.Helper()
	canon, err := canonicalChord(chord)
	if err != nil {
		t.Fatalf("chord %q: %v", chord, err)
	}
	action, _ := table.lookup(stack, canon)
	return action
}

func chatStack() []keyContext {
	return []keyContext{ctxChat, ctxGlobal}
}

func TestClaudeDefaultsProduceClaudeActions(t *testing.T) {
	table := loadForTest(t, "", "", keysSection{}).table
	cases := map[string]keyAction{
		"ctrl+c":        actInterrupt,
		"ctrl+r":        actHistorySearch,
		"shift+tab":     actCycleMode,
		"esc":           actCancel,
		"enter":         actSubmit,
		"ctrl+x ctrl+k": actKillAgents,
		"ctrl+x ctrl+e": actExternalEditor,
		"ctrl+x enter":  actQueueSubmit,
		"alt+p":         "chat:modelPicker",
		"ctrl+y":        "chat:defaultToNewerModel",
		"alt+t":         "chat:thinkingToggle",
		"alt+w":         "chat:workflowKeywordToggle",
		"ctrl+_":        "chat:undo",
		"ctrl+v":        actImagePaste,
		"ctrl+s":        actStash,
		"ctrl+l":        actClearInput,
		"ctrl+b":        "task:background",
		"alt+up":        actDiffFileUp,
		"ctrl+up":       actDiffFileUp,
		"ctrl+]":        actOpenArtifact,
		"ctrl+j":        actNewline,
	}
	for chord, want := range cases {
		if got := actionOf(t, table, chord, chatStack()...); got != want {
			t.Errorf("%s = %q, want %q", chord, got, want)
		}
	}
}

func TestConflictsGoToClaudeAndHerrscherMovesUnderCtrlX(t *testing.T) {
	table := loadForTest(t, "", "", keysSection{}).table
	claudeWins := map[string]keyAction{
		"ctrl+d": actExit,
		"ctrl+t": actToggleTodos,
		"ctrl+o": actToggleTranscript,
		"ctrl+g": actExternalEditor,
	}
	for chord, want := range claudeWins {
		if got := actionOf(t, table, chord, chatStack()...); got != want {
			t.Errorf("%s = %q, want Claude action %q", chord, got, want)
		}
	}
	moved := map[string]keyAction{
		"ctrl+x d": actOpenDiff,
		"ctrl+x s": actOpenSessions,
		"ctrl+x t": actOpenTheme,
		"ctrl+x m": actToggleMouse,
		"ctrl+x l": actNextLink,
		"ctrl+x o": actOpenOrFold,
		"ctrl+x c": actCopyCode,
		"ctrl+x f": actToggleTurnFold,
		"ctrl+x v": actToggleSidebar,
	}
	for chord, want := range moved {
		if got := actionOf(t, table, chord, chatStack()...); got != want {
			t.Errorf("%s = %q, want herrscher action %q", chord, got, want)
		}
	}
}

func TestChordCompletesWithinTimeout(t *testing.T) {
	table := loadForTest(t, "", "", keysSection{}).table
	var state keyPress
	start := time.Now()
	if _, consumed := table.press(chatStack(), &state, "ctrl+x", start, defaultChordTimeout); !consumed {
		t.Fatal("ctrl+x must arm a chord")
	}
	action, consumed := table.press(chatStack(), &state, "ctrl+e", start.Add(2*time.Second), defaultChordTimeout)
	if !consumed || action != actExternalEditor {
		t.Fatalf("ctrl+x ctrl+e within the timeout = (%q, %v), want external editor", action, consumed)
	}
}

func TestChordExpiresAfterTimeout(t *testing.T) {
	table := loadForTest(t, "", "", keysSection{}).table
	var state keyPress
	start := time.Now()
	table.press(chatStack(), &state, "ctrl+x", start, defaultChordTimeout)
	action, consumed := table.press(chatStack(), &state, "ctrl+e", start.Add(4*time.Second), defaultChordTimeout)
	if consumed || action != "" {
		t.Fatalf("ctrl+e after the timeout = (%q, %v), want a plain unbound key", action, consumed)
	}
}

func TestMismatchedKeyAfterPrefixIsSwallowed(t *testing.T) {
	table := loadForTest(t, "", "", keysSection{}).table
	var state keyPress
	now := time.Now()
	table.press(chatStack(), &state, "ctrl+x", now, defaultChordTimeout)
	action, consumed := table.press(chatStack(), &state, "z", now, defaultChordTimeout)
	if !consumed || action != "" {
		t.Fatalf("unmatched key after ctrl+x = (%q, %v), want swallowed with no action", action, consumed)
	}
	if state.pending != "" {
		t.Fatalf("pending chord %q survived a mismatch", state.pending)
	}
}

func TestNullRemovesOneDefaultAndKeepsTheRest(t *testing.T) {
	herrscher := `{"bindings":[{"context":"Chat","bindings":{"ctrl+g":null}}]}`
	table := loadForTest(t, herrscher, "", keysSection{}).table
	if got := actionOf(t, table, "ctrl+g", chatStack()...); got != "" {
		t.Fatalf("ctrl+g still bound to %q after null", got)
	}
	if got := actionOf(t, table, "ctrl+x ctrl+e", chatStack()...); got != actExternalEditor {
		t.Fatalf("ctrl+x ctrl+e = %q after removing ctrl+g", got)
	}
}

func TestUserBindingAddsAndOverridesAfterDefaults(t *testing.T) {
	herrscher := `{"bindings":[{"context":"Chat","bindings":{"ctrl+k":"chat:killAgents","ctrl+x s":"app:openTheme"}}]}`
	load := loadForTest(t, herrscher, "", keysSection{})
	if len(load.problems) != 0 {
		t.Fatalf("unexpected problems: %v", load.problems)
	}
	if got := actionOf(t, load.table, "ctrl+k", chatStack()...); got != actKillAgents {
		t.Fatalf("ctrl+k = %q", got)
	}
	if got := actionOf(t, load.table, "ctrl+x s", chatStack()...); got != actOpenTheme {
		t.Fatalf("ctrl+x s = %q, want the user override", got)
	}
}

func TestClaudeFileIsImportedAndCanBeDisabled(t *testing.T) {
	claude := `{"$schema":"https://www.schemastore.org/claude-code-keybindings.json","bindings":[{"context":"Chat","bindings":{"ctrl+q":"chat:clearScreen"}}]}`
	imported := loadForTest(t, "", claude, keysSection{})
	if got := actionOf(t, imported.table, "ctrl+q", chatStack()...); got != "chat:clearScreen" {
		t.Fatalf("imported ctrl+q = %q", got)
	}
	disabled := false
	off := loadForTest(t, "", claude, keysSection{ImportClaude: &disabled})
	if got := actionOf(t, off.table, "ctrl+q", chatStack()...); got != "" {
		t.Fatalf("ctrl+q bound to %q with import disabled", got)
	}
}

func TestValidationErrorsAreReportedAndTheRestStillApplies(t *testing.T) {
	herrscher := `{"bindings":[
		{"context":"Nowhere","bindings":{"ctrl+a":"chat:submit"}},
		{"context":"Chat","bindings":{"hyper+a":"chat:submit","ctrl+c":"chat:submit","ctrl+d":"app:exit","ctrl+z":"chat:submit","ctrl+e":"chat:nope","ctrl+f":3}},
		{"context":"Chat","bindings":{"ctrl+k":"chat:killAgents"}},
		{"context":"Chat","bindings":{"ctrl+k":"chat:clearScreen"}}
	]}`
	load := loadForTest(t, herrscher, "", keysSection{})
	wantProblems := []string{
		"contexte inconnu",
		"modificateur inconnu",
		"touche non reliable",
		"touche réservée",
		"action inconnue",
		"l’action doit être une chaîne ou null",
		"doublon dans le contexte",
	}
	joined := strings.Join(load.problems, "\n")
	for _, want := range wantProblems {
		if !strings.Contains(joined, want) {
			t.Errorf("no problem mentions %q in:\n%s", want, joined)
		}
	}
	if got := actionOf(t, load.table, "ctrl+k", chatStack()...); got != actKillAgents {
		t.Fatalf("ctrl+k = %q, want the first valid entry", got)
	}
	if got := actionOf(t, load.table, "ctrl+x ctrl+e", chatStack()...); got != actExternalEditor {
		t.Fatalf("defaults lost after validation errors: ctrl+x ctrl+e = %q", got)
	}
}

func TestBrokenJSONIsReportedAndDefaultsSurvive(t *testing.T) {
	load := loadForTest(t, `{"bindings": [`, "", keysSection{})
	if len(load.problems) != 1 || !strings.Contains(load.problems[0], keybindingsFile) {
		t.Fatalf("problems = %v", load.problems)
	}
	if got := actionOf(t, load.table, "ctrl+x ctrl+e", chatStack()...); got != actExternalEditor {
		t.Fatalf("defaults lost after broken file: %q", got)
	}
}

func TestChangedBindingShowsInHelp(t *testing.T) {
	herrscher := `{"bindings":[{"context":"Chat","bindings":{"ctrl+x a":"chat:nextLink"}}]}`
	m := newTestModel()
	m.bindings = loadForTest(t, herrscher, "", keysSection{}).table
	help := ansi.Strip(m.helpView())
	if !strings.Contains(help, "⌃Xa lien suivant") {
		t.Fatalf("changed binding missing from help: %s", help)
	}
}

func TestHelpFitsEveryWidthAndTwentyLines(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m := newTestModel()
		m.width = width
		view := ansi.Strip(m.helpView())
		lines := strings.Split(view, "\n")
		if len(lines) > 20 {
			t.Errorf("width %d: %d lines, more than 20", width, len(lines))
		}
		for _, line := range lines {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width %d: line of %d columns: %q", width, w, line)
			}
		}
	}
}

func TestMainKeyRoutesThroughTable(t *testing.T) {
	m := newTestModel()
	m.bindings = loadForTest(t, `{"bindings":[{"context":"Chat","bindings":{"ctrl+x x":null}}]}`, "", keysSection{}).table
	if _, handled := m.handleMainKey(tea.KeyMsg{Type: tea.KeyCtrlX}); !handled {
		t.Fatal("ctrl+x must arm the chord")
	}
	if _, handled := m.handleMainKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}); !handled {
		t.Fatal("ctrl+x c must be swallowed as a completed chord")
	}
}
