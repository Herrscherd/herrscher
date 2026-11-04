package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func testStyles() uiStyles {
	return newStyles(defaultTheme(), false)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), configDirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), privateFileMode); err != nil {
		t.Fatal(err)
	}
}

func themeModel(t *testing.T, width int) (*model, string) {
	t.Helper()
	dir := t.TempDir()
	m := newModel(&fakeBackend{})
	m.keys = &keyLog{}
	m.handleWindowSize(tea.WindowSizeMsg{Width: width, Height: 40})
	m.loadTheme(dir, func() bool { return true })
	return m, dir
}

func TestBuiltinThemesAndGlyphSetsAreComplete(t *testing.T) {
	ids := make(map[string]bool)
	for _, th := range builtinThemes() {
		ids[th.ID] = true
		if !validColor(string(th.Colors.Text)) || !validColor(string(th.Colors.SelText)) {
			t.Fatalf("theme %s has an invalid colour: %+v", th.ID, th.Colors)
		}
	}
	for _, want := range []string{themeSombre, themeClair, themeTerminal, themeContraste} {
		if !ids[want] {
			t.Fatalf("missing built-in theme %s", want)
		}
	}
	for _, g := range []glyphSet{unicodeGlyphs(), nerdGlyphs(), asciiGlyphs()} {
		if g.cursor == "" || g.failure == "" || len(g.spin) == 0 || g.familyGlyph(familyRead) == "" {
			t.Fatalf("glyph set has an empty role: %+v", g)
		}
	}
}

func TestNoColorDropsForegroundOnEveryRole(t *testing.T) {
	st := newStyles(defaultTheme(), true)
	for name, style := range map[string]lipgloss.Style{"accent": st.accent, "text": st.text, "red": st.red, "green": st.green} {
		if style.GetForeground() != (lipgloss.NoColor{}) {
			t.Fatalf("NO_COLOR must drop the %s foreground", name)
		}
	}
}

func TestNoColorEnvironmentIsRead(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m, _ := themeModel(t, 80)
	if !m.noColor || m.st.accent.GetForeground() != (lipgloss.NoColor{}) {
		t.Fatalf("NO_COLOR=1 must disable colours")
	}
}

func TestPersonalThemeInheritsAndOverrides(t *testing.T) {
	dir := t.TempDir()
	themes := themesDirFor(dir)
	writeFile(t, filepath.Join(themes, "bleu.toml"), "name = \"Bleu\"\nbase = \"clair\"\n[colors]\ntext = \"#010203\"\n")
	writeFile(t, filepath.Join(themes, "bleu-ascii.toml"), "base = \"bleu\"\n[render]\nglyphs = \"ascii\"\n")
	catalog := loadThemeCatalog(themes)
	child, ok := catalog.find("bleu-ascii")
	if !ok {
		t.Fatalf("bleu-ascii must load: %v", catalog.problems)
	}
	clair, _ := builtinTheme("clair")
	if string(child.Colors.Text) != "#010203" || child.Colors.Dim != clair.Colors.Dim {
		t.Fatalf("child must inherit parent colours: %+v", child.Colors)
	}
	if child.Render.Glyphs != glyphsASCII {
		t.Fatalf("child must override glyphs, got %q", child.Render.Glyphs)
	}
}

func TestPersonalThemeCycleAndBadColourAreReported(t *testing.T) {
	dir := t.TempDir()
	themes := themesDirFor(dir)
	writeFile(t, filepath.Join(themes, "a.toml"), "base = \"b\"\n")
	writeFile(t, filepath.Join(themes, "b.toml"), "base = \"a\"\n")
	writeFile(t, filepath.Join(themes, "c.toml"), "[colors]\ntext = \"rouge\"\n")
	catalog := loadThemeCatalog(themes)
	if _, ok := catalog.find("a"); ok {
		t.Fatal("a cyclic theme must not load")
	}
	if len(catalog.problems) < 3 {
		t.Fatalf("cycle and bad colour must be reported, got %v", catalog.problems)
	}
}

func TestStartupAutoThemeFollowsBackground(t *testing.T) {
	catalog := loadThemeCatalog(filepath.Join(t.TempDir(), "none"))
	auto := terminalSection{AutoTheme: boolPtr(true)}
	if got := startupTheme(auto, catalog, func() bool { return false }); got.ID != themeClair {
		t.Fatalf("light background must pick clair, got %s", got.ID)
	}
	if got := startupTheme(auto, catalog, func() bool { return true }); got.ID != themeSombre {
		t.Fatalf("dark background must pick sombre, got %s", got.ID)
	}
	if got := startupTheme(terminalSection{Theme: strPtr(themeTerminal)}, catalog, func() bool { return false }); got.ID != "terminal" {
		t.Fatalf("auto off must keep the chosen theme, got %s", got.ID)
	}
}

func boolPtr(v bool) *bool { return &v }

func TestThemeOverlayFitsEveryWidth(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m, _ := themeModel(t, width)
		m.openTheme()
		for _, line := range strings.Split(m.themeView(), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d: line is %d cells: %q", width, got, ansi.Strip(line))
			}
		}
		if width < themeScreenSideBySide && !strings.Contains(m.themeView(), "aperçu") {
			t.Fatalf("width %d must stack the preview under the list", width)
		}
	}
}

func TestThemeOverlayCommitWritesOnlyChangedLook(t *testing.T) {
	m, dir := themeModel(t, 120)
	m.openTheme()
	m.handleThemeLetter("g")
	m.commitTheme()
	cfg, err := readTerminalConfig(filepath.Join(dir, terminalConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Glyphs == nil || *cfg.Glyphs != glyphsNerd.String() {
		t.Fatalf("changed glyphs must be persisted, got %+v", cfg)
	}
	if cfg.Borders != nil {
		t.Fatalf("unchanged borders must not be written, got %+v", cfg)
	}
	if m.themeScr != nil {
		t.Fatal("commit must close the overlay")
	}
}

func TestThemeOverlayEscRevertsPreview(t *testing.T) {
	m, _ := themeModel(t, 120)
	before := m.theme.ID
	m.openTheme()
	m.themeScr.move(1)
	m.previewTheme()
	m.applyTheme(m.themeScr.origin)
	m.closeTheme()
	if m.theme.ID != before {
		t.Fatalf("esc must restore %s, got %s", before, m.theme.ID)
	}
}

func TestLeaderOpensThemeOverlay(t *testing.T) {
	m, _ := themeModel(t, 120)
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlX})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if m.themeScr == nil {
		t.Fatal("ctrl+x t must open the theme overlay")
	}
}

func TestLeaderTimesOut(t *testing.T) {
	m, _ := themeModel(t, 120)
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlX})
	m.leaderArmed = time.Now().Add(-2 * leaderWindow)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if m.themeScr != nil {
		t.Fatal("a late second key must not run the leader binding")
	}
}

func TestSlashThemeOpensOverlay(t *testing.T) {
	m, _ := themeModel(t, 120)
	m.runLocalSlash("/theme", []string{"theme"})
	if m.themeScr == nil {
		t.Fatal("/theme must open the overlay")
	}
}

func TestAsciiViewIsPureAtEveryWidth(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m, dir := themeModel(t, width)
		writeFile(t, filepath.Join(dir, terminalConfigFile), "[terminal]\nglyphs = \"ascii\"\nborders = \"square\"\n")
		m.loadTheme(dir, func() bool { return true })
		m.openTheme()
		for _, r := range ansi.Strip(m.View()) {
			if r >= 0x80 {
				t.Fatalf("width %d: non-ASCII rune %q in ascii view", width, r)
			}
		}
	}
}

func TestWorkingVerbIsFrench(t *testing.T) {
	if workingVerb != "En cours…" {
		t.Fatalf("workingVerb = %q", workingVerb)
	}
}
