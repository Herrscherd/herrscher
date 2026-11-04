package tui

import (
	"os"
	"path/filepath"
	"reflect"
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

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
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
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600 kept", info.Mode().Perm())
	}

	cfg := loadStatuslineConfig(path)
	if cfg.Source != barSourceScript || cfg.Command != "cat" || cfg.Interval != 300*time.Millisecond {
		t.Errorf("statusline keys reread wrong: %+v", cfg)
	}
	reloaded := newBarState()
	reloaded.config = cfg
	reloaded.restoreFields(cfg.Fields)
	if !reflect.DeepEqual(reloaded.tickedIDs(), bar.tickedIDs()) {
		t.Errorf("ticked order after reload = %v, want %v", reloaded.tickedIDs(), bar.tickedIDs())
	}
	for _, id := range bar.fieldIDs() {
		if reloaded.isShown(id) != bar.isShown(id) {
			t.Errorf("field %s shown = %v after reload, want %v", id, reloaded.isShown(id), bar.isShown(id))
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
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "fields = "); n != 1 {
		t.Errorf("fields written %d times, want 1:\n%s", n, data)
	}
	if strings.Contains(string(data), `"bg"`) {
		t.Errorf("unticked bg must leave the fields list:\n%s", data)
	}
}

func TestMultilineFieldsArrayIsReadAsOneValue(t *testing.T) {
	text := "[statusline]\nfields = [\n  \"mode\", # toujours\n  \"cost\",\n]\n"
	cfg := parseStatuslineSection(text, defaultStatuslineConfig())
	if !reflect.DeepEqual(cfg.Fields, []string{"mode", "cost"}) {
		t.Errorf("fields = %v, want [mode cost]", cfg.Fields)
	}
}

func TestThemeAndStatuslineSettingsSurviveEachOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminal.toml")
	if err := os.WriteFile(path, []byte(terminalTomlWithStatusline), 0o600); err != nil {
		t.Fatal(err)
	}
	theme := "clair"
	glyphs := glyphsNerd
	if err := updateTomlFile(path, terminalSectionName, themeKeyValues(terminalSection{Theme: &theme, Glyphs: &glyphs})...); err != nil {
		t.Fatal(err)
	}
	bar := newBarState()
	bar.config = loadStatuslineConfig(path)
	bar.toggle("cost")
	if err := saveStatuslineChoice(path, &bar); err != nil {
		t.Fatal(err)
	}
	cfg, err := readTerminalConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme == nil || *cfg.Theme != theme || cfg.Glyphs == nil || *cfg.Glyphs != glyphsNerd {
		t.Fatalf("theme keys lost after statusbar write: %+v", cfg)
	}
	reloaded := loadStatuslineConfig(path)
	if !reflect.DeepEqual(reloaded.Fields, bar.tickedIDs()) {
		t.Fatalf("statusbar fields lost after theme write: %v", reloaded.Fields)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# bloc statusline") {
		t.Fatalf("comments must survive: %s", data)
	}
}
