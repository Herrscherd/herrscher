package tui

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

type claudeReferenceRow struct {
	action   keyAction
	keys     []string
	contexts []keyContext
}

func readClaudeReference(t *testing.T) []claudeReferenceRow {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "claude-reference-actions.md"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []claudeReferenceRow
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| Action") || strings.HasPrefix(line, "| ---") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) != 3 {
			t.Fatalf("reference row without three cells: %q", line)
		}
		rows = append(rows, claudeReferenceRow{
			action:   keyAction(strings.TrimSpace(cells[0])),
			keys:     splitReferenceList(cells[1]),
			contexts: toKeyContexts(splitReferenceList(cells[2])),
		})
	}
	return rows
}

func splitReferenceList(cell string) []string {
	var items []string
	for _, item := range strings.Split(cell, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func toKeyContexts(names []string) []keyContext {
	contexts := make([]keyContext, 0, len(names))
	for _, name := range names {
		contexts = append(contexts, keyContext(name))
	}
	return contexts
}

func unloadableReferenceKeys() []string {
	return []string{"ctrl+c", "ctrl+d", "ctrl+m", "ctrl+[", "ctrl+i", "ctrl+h", "capslock"}
}

func TestClaudeReferenceFileLoadsWithoutWarnings(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "claude-reference-keybindings.json"))
	if err != nil {
		t.Fatal(err)
	}
	load := loadForTest(t, "", string(raw), keysSection{})
	if len(load.problems) > 0 {
		t.Fatalf("the Claude reference file must load without warnings: %s", joinProblems(load.problems))
	}
}

func TestClaudeReferenceListMatchesTheReference(t *testing.T) {
	listed := make(map[keyAction]bool)
	for _, action := range claudeReferenceActions() {
		listed[action] = true
	}
	reference := make(map[keyAction]bool)
	for _, row := range readClaudeReference(t) {
		reference[row.action] = true
		if !listed[row.action] {
			t.Errorf("reference action %s is missing from claudeReferenceActions", row.action)
		}
	}
	for action := range listed {
		if !reference[action] {
			t.Errorf("claudeReferenceActions lists %s, which the reference does not have", action)
		}
	}
	if len(listed) != len(reference) {
		t.Fatalf("claudeReferenceActions has %d names, the reference has %d", len(listed), len(reference))
	}
}

func TestClaudeReferenceKeysSitInTheirContexts(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "claude-reference-keybindings.json"))
	if err != nil {
		t.Fatal(err)
	}
	table := loadForTest(t, "", string(raw), keysSection{}).table
	for _, row := range readClaudeReference(t) {
		for _, key := range row.keys {
			if slices.Contains(unloadableReferenceKeys(), key) {
				continue
			}
			checkReferenceKey(t, table, row, key)
		}
	}
}

func checkReferenceKey(t *testing.T, table *keyTable, row claudeReferenceRow, key string) {
	t.Helper()
	canon, err := canonicalChord(key)
	if err != nil {
		t.Fatalf("reference key %q does not canonicalize: %v", key, err)
	}
	for _, ctx := range row.contexts {
		if got := table.chords[ctx][canon]; got != row.action {
			t.Errorf("%s in %s: %s is bound to %q, want %s", canon, ctx, key, got, row.action)
		}
	}
}

func TestEveryClaudeReferenceActionIsAccepted(t *testing.T) {
	for _, action := range claudeReferenceActions() {
		file := `{"bindings":[{"context":"Global","bindings":{"ctrl+x z":"` + string(action) + `"}}]}`
		if load := loadForTest(t, "", file, keysSection{}); len(load.problems) > 0 {
			t.Errorf("reference action %s is rejected: %s", action, joinProblems(load.problems))
		}
	}
}

func TestEveryClaudeReferenceContextIsAccepted(t *testing.T) {
	for _, ctx := range claudeReferenceContexts() {
		file := `{"bindings":[{"context":"` + string(ctx) + `","bindings":{"ctrl+x z":"app:redraw"}}]}`
		if load := loadForTest(t, "", file, keysSection{}); len(load.problems) > 0 {
			t.Errorf("reference context %s is rejected: %s", ctx, joinProblems(load.problems))
		}
	}
}

func actionSet(actions []keyAction) map[keyAction]bool {
	set := make(map[keyAction]bool, len(actions))
	for _, action := range actions {
		set[action] = true
	}
	return set
}

func readmeActionNames(line string) []keyAction {
	var names []keyAction
	for _, match := range regexp.MustCompile("`([a-zA-Z]+:[a-zA-Z]+)`").FindAllStringSubmatch(line, -1) {
		names = append(names, keyAction(match[1]))
	}
	return names
}

func readmeNotYetLines(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	inSection := false
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "#### Not yet" {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if strings.HasPrefix(line, "#### ") {
			break
		}
		lines = append(lines, line)
	}
	return lines
}

func readmeNotYet(t *testing.T) ([]keyAction, []keyAction) {
	t.Helper()
	var table, all []keyAction
	for _, line := range readmeNotYetLines(t) {
		names := readmeActionNames(line)
		if strings.HasPrefix(line, "|") {
			table = append(table, names...)
		}
		all = append(all, names...)
	}
	return table, all
}

func TestReadmeNotYetNamesTheKeysThatHaveNoEffect(t *testing.T) {
	var table []string
	for _, line := range readmeNotYetLines(t) {
		if strings.HasPrefix(line, "|") {
			table = append(table, line)
		}
	}
	for _, key := range []string{"⌃X ⌃A", "⌃X ⇥", "⌃⇧B", "⌃]", "⌥P", "⌥O", "⌥W", "⌘K"} {
		if !slices.ContainsFunc(table, func(row string) bool { return strings.Contains(row, "`"+key+"`") }) {
			t.Errorf("the Not yet table must name the key %s", key)
		}
	}
}

func TestReadmeNotYetTableListsTheFourteenSilentActions(t *testing.T) {
	table, _ := readmeNotYet(t)
	want := defaultsWithoutEffect()
	if len(table) != len(want) || !maps.Equal(actionSet(table), actionSet(want)) {
		t.Fatalf("the Not yet table must list exactly defaultsWithoutEffect: got %v, want %v", table, want)
	}
}

func TestReadmeNotYetListsEveryUnhandledReferenceAction(t *testing.T) {
	tables, err := newDispatchTables()
	if err != nil {
		t.Fatal(err)
	}
	handled := actionSet(boundActions(tables))
	var unhandled []keyAction
	for _, action := range claudeReferenceActions() {
		if !handled[action] {
			unhandled = append(unhandled, action)
		}
	}
	_, all := readmeNotYet(t)
	if len(all) != len(actionSet(all)) {
		t.Fatal("the Not yet list repeats an action name")
	}
	if !maps.Equal(actionSet(all), actionSet(unhandled)) {
		t.Fatalf("the Not yet list must hold the %d unhandled reference names, has %d", len(unhandled), len(all))
	}
}
