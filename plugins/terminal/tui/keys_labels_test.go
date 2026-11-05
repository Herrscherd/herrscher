package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCycleModeLabelsFollowTheKeyTable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	file := `{"bindings":[{"context":"Chat","bindings":{"shift+tab":null,"alt+m":"chat:cycleMode"}},{"context":"Plan","bindings":{"shift+tab":null,"alt+m":"chat:cycleMode"}}]}`
	if err := os.WriteFile(filepath.Join(dir, keybindingsFile), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	m := modeSession(&fakeBackend{sessions: namedSession()})
	before := ansi.Strip(m.modeChip(m.tabs["a"]))
	m.loadKeys(dir)
	hint := m.hintIn(actCycleMode, ctxPlan)
	if hint == "" || hint == strings.TrimSpace(strings.TrimPrefix(before, "Plan")) {
		t.Fatalf("rebound cycleMode hint = %q, want a label different from the default", hint)
	}
	if chip := ansi.Strip(m.modeChip(m.tabs["a"])); !strings.HasSuffix(chip, hint) {
		t.Fatalf("mode chip = %q, want it to end with the rebound hint %q", chip, hint)
	}
	if row := ansi.Strip(m.statuslineRow(barModeID, false, 200)); !strings.Contains(row, hint) {
		t.Fatalf("status line row = %q, want the rebound hint %q", row, hint)
	}
}
