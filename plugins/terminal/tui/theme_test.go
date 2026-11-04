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

func themeSized(t *testing.T, width, height int) *model {
	t.Helper()
	m, dir := themeModel(t, width)
	m.handleWindowSize(tea.WindowSizeMsg{Width: width, Height: height})
	m.loadTheme(dir, func() bool { return true })
	m.openTheme()
	return m
}

func TestThemeScreenFitsTheWindow(t *testing.T) {
	for _, height := range []int{20, 30, 40} {
		for _, width := range []int{60, 80, 100, 120, 200, 240} {
			m := themeSized(t, width, height)
			lines := strings.Split(m.View(), "\n")
			if len(lines) > height {
				t.Fatalf("%dx%d: view is %d lines", width, height, len(lines))
			}
			for _, line := range lines {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("%dx%d: line is %d cells: %q", width, height, got, ansi.Strip(line))
				}
			}
		}
	}
}

func TestThemeScreenHeaderAndHelpFitOnOneLine(t *testing.T) {
	for _, height := range []int{20, 30} {
		plain := ansi.Strip(themeSized(t, 80, height).View())
		if !strings.Contains(plain, "Thème · /theme · aperçu en direct") {
			t.Fatalf("%d lines: header must name the screen and the live preview", height)
		}
		for _, want := range []string{"↑↓ aperçu en direct · ⏎ garder · échap revenir", "⌃X puis t : thème suivant"} {
			if !lineContains(plain, want) {
				t.Fatalf("%d lines: help %q must sit on one line", height, want)
			}
		}
	}
}

func lineContains(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

func TestThemeScreenListShowsDescriptionAndCurrentMark(t *testing.T) {
	plain := ansi.Strip(themeSized(t, 120, 30).View())
	if !lineContains(plain, "✓ Sombre") {
		t.Fatal("the kept theme must carry the ✓ mark")
	}
	if !lineContains(plain, "fond sombre · défaut") || !lineContains(plain, "16 couleurs ANSI du terminal") {
		t.Fatal("built-in themes must show their description")
	}
}

func TestThemeScreenListsPersonalThemeWhenFileExists(t *testing.T) {
	m, dir := themeModel(t, 120)
	writeFile(t, filepath.Join(themesDirFor(dir), themePersonalFile), "name = \"Mon thème\"\nbase = \"sombre\"\n")
	m.loadTheme(dir, func() bool { return true })
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.openTheme()
	plain := ansi.Strip(m.View())
	if strings.Count(plain, "Mon thème") != 1 || !lineContains(plain, "themes/perso.toml") {
		t.Fatalf("perso.toml must be listed once with its file name:\n%s", plain)
	}
}

func TestThemeScreenShowsPersonalPlaceholderWithoutFile(t *testing.T) {
	plain := ansi.Strip(themeSized(t, 120, 30).View())
	if !lineContains(plain, "Mon thème") || !lineContains(plain, "themes/perso.toml") {
		t.Fatalf("the personal theme must stay in the list before its file exists:\n%s", plain)
	}
}

func TestThemeScreenRefusesToKeepMissingPersonalTheme(t *testing.T) {
	m := themeSized(t, 120, 30)
	m.themeScr.move(len(m.themeScr.themes))
	m.commitTheme()
	if m.themeScr == nil {
		t.Fatal("a missing personal theme must not close the screen")
	}
	if !strings.Contains(m.flash, themePersonalFile) {
		t.Fatalf("the refusal must name the missing file, got %q", m.flash)
	}
}

func TestThemeScreenRenderOptionsAreVisibleAndCycle(t *testing.T) {
	m := themeSized(t, 120, 30)
	plain := ansi.Strip(m.View())
	for _, want := range []string{"glyphes", "unicode", "arrondies", "compacte", "[✓] gras", "[✓] italique", "[ ] auto"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("RENDU must show %q:\n%s", want, plain)
		}
	}
	m.handleThemeLetter("b")
	m.handleThemeLetter("d")
	m.handleThemeLetter("w")
	m.handleThemeLetter("i")
	m.handleThemeLetter("a")
	if m.themeScr.look.Borders != borderSquare || m.themeScr.look.Density != densityCompact || m.themeScr.look.Bold || m.themeScr.look.Italic || !m.themeScr.auto {
		t.Fatalf("the option keys must change the look: %+v auto=%v", m.themeScr.look, m.themeScr.auto)
	}
}

func TestThemeScreenPreviewElementsAreShown(t *testing.T) {
	plain := ansi.Strip(themeSized(t, 120, 30).View())
	for _, want := range []string{"Aperçu · Sombre · gardé", "✓ ajouté", "✗ retiré", "● attente", "> ligne sélectionnée", "· notice", "erreur", "ctx"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("preview must show %q:\n%s", want, plain)
		}
	}
}

func TestThemeScreenPreviewStepsAsideOnNarrowWindows(t *testing.T) {
	if !strings.Contains(ansi.Strip(themeSized(t, 60, 30).View()), "Aperçu") {
		t.Fatal("a 30-line window must stack the preview under the list")
	}
	if strings.Contains(ansi.Strip(themeSized(t, 60, 20).View()), "Aperçu") {
		t.Fatal("a 20-line window must hide the preview rather than overflow")
	}
}

func TestThemeScreenCtrlEOpensPersonalFile(t *testing.T) {
	m := themeSized(t, 120, 30)
	if cmd := m.handleThemeKey(tea.KeyMsg{Type: tea.KeyCtrlE}); cmd == nil {
		t.Fatal("ctrl+e must open the personal theme file")
	}
	if _, err := os.Stat(filepath.Join(themesDirFor(m.themeDir), themePersonalFile)); err != nil {
		t.Fatalf("ctrl+e must create perso.toml: %v", err)
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
	if m.themeScr == nil || !m.themeScr.saved {
		t.Fatal("commit must keep the screen open on the saved line")
	}
	m.handleThemeKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.themeScr != nil {
		t.Fatal("esc must close the screen after a commit")
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

func TestThemeScreenShowsRenderSection(t *testing.T) {
	plain := ansi.Strip(themeSized(t, 120, 30).View())
	for _, want := range []string{"police", "celle du terminal", "un TUI ne choisit pas sa police : seule une appli", "qui dessine sa propre fenêtre le peut"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("RENDU must show %q:\n%s", want, plain)
		}
	}
}

func TestThemeScreenShowsDetectionBlock(t *testing.T) {
	m, dir := themeModel(t, 120)
	writeFile(t, filepath.Join(dir, terminalConfigFile), "[terminal]\nauto_theme = true\n")
	m.loadTheme(dir, func() bool { return true })
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.openTheme()
	plain := ansi.Strip(m.View())
	for _, want := range []string{"sombre · lu par OSC 11 au démarrage", "NO_COLOR respecté : marqueurs et gras seuls", "fond sombre → Sombre, clair → Clair"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("DÉTECTION must show %q:\n%s", want, plain)
		}
	}
}

func TestThemeScreenDetectionReportsUnknownWithoutProbe(t *testing.T) {
	m, dir := themeModel(t, 120)
	m.loadTheme(dir, func() bool { panic("probe must not run without auto") })
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.openTheme()
	if plain := ansi.Strip(m.View()); !strings.Contains(plain, "inconnu") {
		t.Fatalf("without auto the background must read inconnu:\n%s", plain)
	}
}

func TestThemeScreenPreviewUsesRealRenderers(t *testing.T) {
	plain := ansi.Strip(themeSized(t, 120, 40).View())
	for _, want := range []string{"Read", "Edit", "+12", "−4", "Bash", "go vet ./...", "refus dans 4:12", "git push origin feat/tui-v2", "41%", "85%", "97%", "En cours…"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("preview must show %q:\n%s", want, plain)
		}
	}
}

func TestThemeScreenTerminalTomlBlock(t *testing.T) {
	m := themeSized(t, 120, 40)
	plain := ansi.Strip(m.View())
	if !lineContains(plain, "[terminal]") || !lineContains(plain, "theme      = \"sombre\"") || !lineContains(plain, "auto_theme = false") {
		t.Fatalf("terminal.toml block must show the section and its keys:\n%s", plain)
	}
	if strings.Contains(plain, "enregistré à l’instant") {
		t.Fatal("the saved line must not show before a commit")
	}
	m.commitTheme()
	if !strings.Contains(ansi.Strip(m.View()), "enregistré à l’instant") {
		t.Fatal("the saved line must show right after a commit")
	}
}

func TestThemePersonalTomlBlockListsTokens(t *testing.T) {
	plain := ansi.Strip(themeSized(t, 120, 60).View())
	for _, token := range []string{"name", "base", "accent", "add", "del", "wait", "glyphs", "borders", "density", "bold", "italic"} {
		if !lineContains(plain, token+" ") {
			t.Fatalf("perso.toml block must list %q:\n%s", token, plain)
		}
	}
	if !strings.Contains(plain, "hérite de base, ne nomme que ce qui change") {
		t.Fatal("perso.toml block must explain inheritance")
	}
}

func TestThemePreviewDropsBottomBlocksFirst(t *testing.T) {
	tall := ansi.Strip(themeSized(t, 120, 60).View())
	short := ansi.Strip(themeSized(t, 120, 22).View())
	if !strings.Contains(tall, "tui/theme.go  tui/rail.go") {
		t.Fatalf("a tall window must show the file line:\n%s", tall)
	}
	if !strings.Contains(tall, "hérite de base") {
		t.Fatalf("a tall window must show the perso.toml block:\n%s", tall)
	}
	if strings.Contains(short, "hérite de base") && !strings.Contains(short, "[terminal]") {
		t.Fatalf("perso.toml must go before terminal.toml when short:\n%s", short)
	}
}
