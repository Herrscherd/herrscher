package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const terminalTomlWithStatusline = `# réglages du terminal
[theme]
accent = "#ff8800" # couleur d'accent
# bloc statusline
[statusline]
# ma commande
command = "cat"
interval = "300ms"

[other]
fields = ["cost"]
`

func readConfigText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertKeepsUnrelatedText(t *testing.T, path string) {
	t.Helper()
	text := readConfigText(t, path)
	for _, keep := range []string{
		"# réglages du terminal",
		`accent = "#ff8800" # couleur d'accent`,
		"# bloc statusline",
		"# ma commande",
		`command = "cat"`,
		`interval = "300ms"`,
		"[other]\nfields = [\"cost\"]",
	} {
		if !strings.Contains(text, keep) {
			t.Errorf("terminal.toml lost %q:\n%s", keep, text)
		}
	}
}

func TestStatuslineChoiceSurvivesARoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminal.toml")
	if err := os.WriteFile(path, []byte(terminalTomlWithStatusline), 0o600); err != nil {
		t.Fatal(err)
	}
	bar := newBarState()
	bar.config = loadStatuslineConfig(path)
	bar.toggle("cost")
	bar.toggle("dur")
	bar.moveField(1, 1)
	bar.config.Source = barSourceScript
	if err := saveStatuslineChoice(path, &bar); err != nil {
		t.Fatalf("save: %v", err)
	}
	assertKeepsUnrelatedText(t, path)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600 kept", info.Mode().Perm())
	}
	assertStatuslineReread(t, path, &bar)
}

func assertStatuslineReread(t *testing.T, path string, bar *barState) {
	t.Helper()
	cfg := loadStatuslineConfig(path)
	if cfg.Source != barSourceScript || cfg.Command != "cat" || cfg.Interval != 300*time.Millisecond {
		t.Errorf("statusline keys reread wrong: %+v", cfg)
	}
	reloaded := newBarState()
	reloaded.config = cfg
	reloaded.restoreFields(cfg.Fields)
	if !slices.Equal(reloaded.tickedIDs(), bar.tickedIDs()) {
		t.Errorf("ticked order after reload = %v, want %v", reloaded.tickedIDs(), bar.tickedIDs())
	}
	for _, fieldID := range bar.fieldIDs() {
		if reloaded.isShown(fieldID) != bar.isShown(fieldID) {
			t.Errorf("field %s shown = %v after reload, want %v", fieldID, reloaded.isShown(fieldID), bar.isShown(fieldID))
		}
	}
	if reloaded.isShown("cost") || !reloaded.isShown("dur") {
		t.Errorf("cost must stay unticked and dur ticked after reload")
	}
}

func TestSavingTwiceRewritesTheSameKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminal.toml")
	bar := newBarState()
	bar.config.Source = barSourceBuiltin
	if err := saveStatuslineChoice(path, &bar); err != nil {
		t.Fatalf("first save: %v", err)
	}
	bar.toggle("bg")
	if err := saveStatuslineChoice(path, &bar); err != nil {
		t.Fatalf("second save: %v", err)
	}
	text := readConfigText(t, path)
	if n := strings.Count(text, "fields = "); n != 1 {
		t.Errorf("fields written %d times, want 1:\n%s", n, text)
	}
	if strings.Contains(text, `"bg"`) {
		t.Errorf("unticked bg must leave the fields list:\n%s", text)
	}
}

func TestMultilineFieldsArrayIsReadAsOneValue(t *testing.T) {
	text := "[statusline]\nfields = [\n  \"mode\", # toujours\n  \"cost\",\n]\n"
	cfg := parseStatuslineSection(text, defaultStatuslineConfig())
	if !slices.Equal(cfg.Fields, []string{"mode", "cost"}) {
		t.Errorf("fields = %v, want [mode cost]", cfg.Fields)
	}
}

func TestThemeThenStatusbarSettingsBothSurvive(t *testing.T) {
	m, dir := themeModel(t, 120)
	path := filepath.Join(dir, terminalConfigFile)
	writeFile(t, path, terminalTomlWithStatusline)
	m.loadTheme(dir, func() bool { return true })

	themeName := "clair"
	glyphs := glyphsNerd.String()
	if err := m.saveThemeConfig(terminalSection{Theme: &themeName, Glyphs: &glyphs}); err != nil {
		t.Fatalf("theme save: %v", err)
	}
	bar := newBarState()
	bar.file = m.terminalConfigPath()
	bar.config = loadStatuslineConfig(bar.file)
	bar.toggle("cost")
	if err := saveStatuslineChoice(bar.file, &bar); err != nil {
		t.Fatalf("statusbar save: %v", err)
	}

	if bar.file != path {
		t.Fatalf("statusbar and theme must share one file: %s vs %s", bar.file, path)
	}
	cfg, err := readTerminalConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme == nil || *cfg.Theme != themeName || cfg.Glyphs == nil || *cfg.Glyphs != glyphs {
		t.Fatalf("theme keys lost after statusbar write: %+v", cfg)
	}
	if reloaded := loadStatuslineConfig(path); !slices.Equal(reloaded.Fields, bar.tickedIDs()) {
		t.Fatalf("statusbar fields lost after theme write: %v", reloaded.Fields)
	}
	assertKeepsUnrelatedText(t, path)
}
