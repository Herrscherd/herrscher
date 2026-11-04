package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestTerminalTomlStatuslineSectionIsReadTolerantly(t *testing.T) {
	text := `
[terminal]
theme = "sombre"
auto_theme = true

[statusline]
source = "script"
command = "~/bin/bar.sh --json"
interval = "300ms"

[other]
source = "builtin"
`
	cfg := parseStatuslineSection(text, defaultStatuslineConfig())
	if cfg.Source != barSourceScript {
		t.Errorf("source = %q, want script", cfg.Source)
	}
	if cfg.Command != "~/bin/bar.sh --json" {
		t.Errorf("command = %q", cfg.Command)
	}
	if cfg.Interval != 300*time.Millisecond {
		t.Errorf("interval = %v, want 300ms", cfg.Interval)
	}
}

func TestMissingStatuslineSectionKeepsTheBuiltinBar(t *testing.T) {
	cfg := parseStatuslineSection("[terminal]\ntheme = \"clair\"\n", defaultStatuslineConfig())
	if cfg.Source != barSourceBuiltin {
		t.Errorf("source = %q, want builtin", cfg.Source)
	}
}

func TestScriptFirstLineBecomesTheBar(t *testing.T) {
	m, tb := barModelAt(t, "")
	m.bar.config.Source = barSourceScript
	line, err := execStatusline(`printf 'bar perso\nsecond\n'`, []byte("{}"))
	if err != nil {
		t.Fatalf("a script printing a line must not fail: %v", err)
	}
	m.receiveScript(scriptDoneMsg{line: line})
	if got := m.statusBar(tb, 120); got != "bar perso" {
		t.Errorf("bar = %q, want the first line of the script", got)
	}
}

func TestScriptReceivesTheStateAsJSON(t *testing.T) {
	line, err := execStatusline("cat", []byte(`{"mode":"Manuel"}`))
	if err != nil {
		t.Fatalf("cat must succeed: %v", err)
	}
	if !strings.Contains(line, `"mode":"Manuel"`) {
		t.Errorf("the script must read the state on stdin: %q", line)
	}
}

func TestSlowScriptFallsBackToTheBuiltinBarWithAMark(t *testing.T) {
	m, tb := barModelAt(t, "")
	m.bar.config.Source = barSourceScript
	start := time.Now()
	_, err := execStatusline("sleep 5", []byte("{}"))
	if err == nil {
		t.Fatal("a script past its one second must fail")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("the script must be cut at one second, took %v", elapsed)
	}
	m.receiveScript(scriptDoneMsg{err: err})
	bar := m.statusBar(tb, 120)
	if !strings.Contains(bar, "41%") {
		t.Errorf("a failed script must show the builtin bar: %q", bar)
	}
	if !strings.HasSuffix(bar, unicodeGlyphs().failure) {
		t.Errorf("a failed script must end with the discreet mark: %q", bar)
	}
	if lipgloss.Width(bar) > 120 {
		t.Errorf("the fallback must still fit: %d cells", lipgloss.Width(bar))
	}
}

func TestEmptyScriptOutputIsAFailure(t *testing.T) {
	if _, err := execStatusline("true", []byte("{}")); err == nil {
		t.Fatal("a script printing nothing must count as a failure")
	}
}
