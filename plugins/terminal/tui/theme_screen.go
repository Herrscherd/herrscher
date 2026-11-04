package tui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	themeScreenSideBySide = 100
	themeLeftWidth        = 56
	themeLabelWidth       = 16
	themeKeyColumn        = 11
	themeGaugeCells       = 6
	themeHeadGap          = 2
	themePersonalID       = "perso"
	themePersonalFile     = themePersonalID + themeFileExt
	themePersonalName     = "Mon thème"
	themePersonalMissing  = "themes/" + themePersonalFile + " absent : e ou ⌃E pour le créer"
)

type themeScreen struct {
	themes   []Theme
	paths    map[string]string
	cursor   int
	origin   Theme
	look     renderOptions
	auto     bool
	problems []string
}

type themeEditedMsg struct{ err error }

func newThemeScreen(current Theme, catalog themeCatalog, auto bool) *themeScreen {
	screen := &themeScreen{origin: current, look: current.Render, auto: auto}
	screen.refresh(catalog)
	if index := catalog.indexOf(current.ID); index >= 0 {
		screen.cursor = index
	}
	return screen
}

func (s *themeScreen) refresh(catalog themeCatalog) {
	selected := s.selectedID()
	s.themes = append([]Theme(nil), catalog.themes...)
	if catalog.indexOf(themePersonalID) < 0 {
		placeholder := s.origin
		placeholder.ID = themePersonalID
		placeholder.Label = themePersonalName
		s.themes = append(s.themes, placeholder)
	}
	s.paths = catalog.paths
	s.problems = catalog.problems
	s.cursor = 0
	for index := range s.themes {
		if s.themes[index].ID == selected {
			s.cursor = index
		}
	}
}

func (s *themeScreen) selectedID() string {
	if len(s.themes) == 0 {
		return s.origin.ID
	}
	return s.themes[s.cursor].ID
}

func (s *themeScreen) preview() Theme {
	current := s.themes[s.cursor]
	current.Render = s.look
	return current
}

func (s *themeScreen) move(delta int) {
	s.cursor = min(max(s.cursor+delta, 0), len(s.themes)-1)
}

func (s *themeScreen) keepable() bool {
	themeID := s.selectedID()
	if _, builtin := builtinTheme(themeID); builtin {
		return true
	}
	_, installed := s.paths[themeID]
	return installed
}

func themeDescription(themeID string) (string, bool) {
	switch themeID {
	case themeSombre:
		return "fond sombre · défaut", true
	case themeClair:
		return "le même sur fond clair", true
	case themeTerminal:
		return "16 couleurs ANSI du terminal", true
	case themeContraste:
		return "écarts maximaux", true
	}
	return "", false
}

func (s *themeScreen) detail(themeID string) string {
	if text, ok := themeDescription(themeID); ok {
		return text
	}
	if path, ok := s.paths[themeID]; ok {
		return "themes/" + filepath.Base(path)
	}
	return "themes/" + themePersonalFile
}

func (s *themeScreen) baseID() string {
	if s.origin.ID == themePersonalID {
		return defaultThemeID
	}
	return s.origin.ID
}

func (m *model) openTheme() {
	if m.themeScr != nil {
		return
	}
	m.themeScr = newThemeScreen(m.theme, m.themeCat, m.themeCfg.autoTheme())
	m.relayout()
}

func (m *model) closeTheme() {
	m.themeScr = nil
	m.relayout()
}

func (m *model) previewTheme() {
	m.applyTheme(m.themeScr.preview())
}

func (m *model) handleThemeKey(msg tea.KeyMsg) tea.Cmd {
	screen := m.themeScr
	if m.overlayNav(msg, func(delta int) {
		screen.move(delta)
		m.previewTheme()
	}) {
		return nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.applyTheme(screen.origin)
		m.closeTheme()
		return nil
	case tea.KeyEnter:
		m.commitTheme()
		return nil
	case tea.KeyCtrlE:
		return m.editThemeFile()
	case tea.KeyCtrlC:
		return m.overlayCtrlC()
	}
	return m.handleThemeLetter(msg.String())
}

func (m *model) handleThemeLetter(key string) tea.Cmd {
	screen := m.themeScr
	switch key {
	case "g":
		screen.look.Glyphs = nextOption(glyphModes(), screen.look.Glyphs)
	case "b":
		screen.look.Borders = nextOption(borderStyles(), screen.look.Borders)
	case "d":
		screen.look.Density = nextOption(densities(), screen.look.Density)
	case "w":
		screen.look.Bold = !screen.look.Bold
	case "i":
		screen.look.Italic = !screen.look.Italic
	case "a":
		screen.auto = !screen.auto
	case "e":
		return m.editThemeFile()
	default:
		return nil
	}
	m.previewTheme()
	return nil
}

func (m *model) commitTheme() {
	screen := m.themeScr
	if !screen.keepable() {
		m.flash = themePersonalMissing
		return
	}
	preview := screen.preview()
	base := screen.themes[screen.cursor]
	themeName, auto := preview.ID, screen.auto
	section := terminalSection{Theme: &themeName, AutoTheme: &auto}
	section.Glyphs = overrideSetting(preview.Render.Glyphs.String(), base.Render.Glyphs.String())
	section.Borders = overrideSetting(preview.Render.Borders.String(), base.Render.Borders.String())
	section.Density = overrideSetting(preview.Render.Density.String(), base.Render.Density.String())
	section.Bold = overrideSetting(preview.Render.Bold, base.Render.Bold)
	section.Italic = overrideSetting(preview.Render.Italic, base.Render.Italic)
	if err := m.saveThemeConfig(section); err != nil {
		m.flash = "thème non enregistré : " + err.Error()
	}
	m.closeTheme()
}

func overrideSetting[Choice comparable](value, inherited Choice) *Choice {
	if value == inherited {
		return nil
	}
	return &value
}

func (m *model) editThemeFile() tea.Cmd {
	path, err := m.themeFileForEdit()
	if err != nil {
		m.flash = "thème non ouvert : " + err.Error()
		return nil
	}
	return tea.ExecProcess(editorCommand(m.runContext(), path), func(err error) tea.Msg { return themeEditedMsg{err: err} })
}

func (m *model) themeFileForEdit() (string, error) {
	if path, ok := m.themeCat.paths[themePersonalID]; ok {
		return path, nil
	}
	dir := themesDirFor(m.themeDir)
	if err := os.MkdirAll(dir, configDirMode); err != nil {
		return "", err
	}
	path := filepath.Join(dir, themePersonalFile)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	body := fmt.Sprintf("name = %q\nbase = %q\n", themePersonalName, m.themeScr.baseID())
	return path, os.WriteFile(path, []byte(body), privateFileMode)
}

func (m *model) handleThemeEdited(msg themeEditedMsg) {
	m.reloadThemes()
	if msg.err != nil {
		m.flash = "éditeur : " + msg.err.Error()
	}
}

func (m *model) themeScreenView() string {
	top := m.railLines()
	bottom := m.statusZone()
	rows := max(m.height-len(top)-len(bottom), 0)
	lines := make([]string, 0, m.height)
	lines = append(lines, top...)
	lines = append(lines, m.themeBody(m.innerWidth(), rows)...)
	lines = append(lines, bottom...)
	return m.st.finish(strings.Join(clipLines(lines, m.height), "\n"))
}

func (m *model) themeBody(width, rows int) []string {
	if width >= themeScreenSideBySide {
		left := m.themeControlsFitting(themeLeftWidth, rows)
		right := m.themePreview(width - themeLeftWidth - 1)
		return themeColumns(left, right, themeLeftWidth, m.st.dim.Render(m.st.glyphs.divider), rows)
	}
	body := m.themeControlsFitting(width, rows)
	if preview := m.themePreview(width); rows-len(body) > len(preview) {
		body = append(body, "")
		body = append(body, preview...)
	}
	return clipLines(body, rows)
}

func (m *model) themeControlsFitting(width, rows int) []string {
	full := m.themeControls(width, true)
	if len(full) <= rows {
		return full
	}
	return m.themeControls(width, false)
}

func (m *model) themeControls(width int, gaps bool) []string {
	styles := m.st
	screen := m.themeScr
	var lines []string
	gap := func() {
		if gaps {
			lines = append(lines, "")
		}
	}
	lines = append(lines, styles.strong.Render("Thème")+styles.dim.Render(" · /theme · aperçu en direct"))
	lines = append(lines, m.themeListLines(width)...)
	if len(screen.problems) > 0 {
		lines = append(lines, styles.dim.Render("thème ignoré : "+screen.problems[0]))
	}
	gap()
	lines = append(lines,
		styles.dim.Render("↑↓ aperçu en direct · ⏎ garder · échap revenir"),
		styles.dim.Render("⌃X puis t : thème suivant, depuis n’importe où"),
		styles.dim.Render("e ou ⌃E éditer themes/"+themePersonalFile),
	)
	gap()
	lines = append(lines,
		themeSectionHead(styles, "Rendu", "dans le thème", width),
		themeOptionRow(styles, "glyphes", glyphModes(), screen.look.Glyphs, glyphLabel, "g"),
		themeOptionRow(styles, "bordures", borderStyles(), screen.look.Borders, borderLabel, "b"),
		themeOptionRow(styles, "densité", []density{densityCompact, densityNormal}, screen.look.Density, densityLabel, "d"),
		themeFlagRow(styles, screen.look.Bold, screen.look.Italic),
	)
	gap()
	lines = append(lines,
		themeSectionHead(styles, "Détection", "", width),
		themeAutoRow(styles, screen.auto),
	)
	for index := range lines {
		lines[index] = truncate(lines[index], width)
	}
	return lines
}

func (m *model) themeListLines(width int) []string {
	screen := m.themeScr
	lines := make([]string, 0, len(screen.themes))
	for index := range screen.themes {
		theme := &screen.themes[index]
		lines = append(lines, m.themeListLine(theme, index == screen.cursor, theme.ID == screen.origin.ID, width))
	}
	return lines
}

func (m *model) themeListLine(theme *Theme, selected, current bool, width int) string {
	styles := m.st
	glyphs := styles.glyphs
	head, mark := "  ", glyphs.open
	if selected {
		head = glyphs.cursor + " "
	}
	if current {
		mark = glyphs.success
	}
	label := padTo(theme.Label, themeLabelWidth)
	detail := m.themeScr.detail(theme.ID)
	if selected {
		return styles.overlaySel.Render(padTo(truncate(head+mark+" "+label+"  "+detail, width), width))
	}
	markStyle := styles.dim
	if current {
		markStyle = styles.green.Bold(true)
	}
	return padTo(truncate(styles.dim.Render(head)+markStyle.Render(mark)+" "+styles.text.Render(label)+styles.dim.Render(detail), width), width)
}

func themeSectionHead(styles uiStyles, title, extra string, width int) string {
	used := lipgloss.Width(title) + themeHeadGap
	head := styles.strong.Render(title)
	if extra != "" {
		used += lipgloss.Width(extra) + 1
		head += styles.dim.Render(" " + extra)
	}
	rule := strings.Repeat(styles.glyphs.ruleChar, max(width-used, 1))
	return head + " " + styles.dim.Render(rule)
}

func themeOptionRow[Choice comparable](styles uiStyles, label string, choices []Choice, current Choice, name func(Choice) string, key string) string {
	var row strings.Builder
	row.WriteString("   " + padTo(label, themeKeyColumn))
	for _, choice := range choices {
		if choice == current {
			row.WriteString(styles.chip.Render(name(choice)))
		} else {
			row.WriteString(styles.dim.Render(" " + name(choice) + " "))
		}
		row.WriteString(" ")
	}
	row.WriteString(styles.dim.Render(key))
	return row.String()
}

func themeFlagRow(styles uiStyles, bold, italic bool) string {
	return "   " + padTo("texte", themeKeyColumn) + themeFlag(styles, bold, "gras") + "  " +
		themeFlag(styles, italic, "italique") + "  " + styles.dim.Render("w i")
}

func themeAutoRow(styles uiStyles, auto bool) string {
	return "   " + themeFlag(styles, auto, "auto") +
		styles.dim.Render(" : fond sombre → Sombre, clair → Clair") + "  " + styles.dim.Render("a")
}

func themeFlag(styles uiStyles, on bool, label string) string {
	if on {
		return styles.text.Render(styles.glyphs.ticked + " " + label)
	}
	return styles.dim.Render(styles.glyphs.unticked + " " + label)
}

func glyphLabel(mode glyphMode) string {
	if mode == glyphsNerd {
		return "nerd font"
	}
	return mode.String()
}

func borderLabel(style borderStyle) string {
	switch style {
	case borderSquare:
		return "carrées"
	case borderNone:
		return "aucune"
	case borderRounded:
		return "arrondies"
	}
	return "arrondies"
}

func densityLabel(level density) string {
	if level == densityCompact {
		return "compacte"
	}
	return "normale"
}

func (m *model) themePreview(width int) []string {
	styles := m.st
	glyphs := styles.glyphs
	screen := m.themeScr
	shown := screen.preview()
	kept := " · pas encore gardé"
	if shown.ID == screen.origin.ID {
		kept = " · gardé"
	}
	lines := []string{
		styles.strong.Render("Aperçu") + styles.dim.Render(" · "+shown.Label+kept),
		"",
		styles.text.Render("Entrée pendant un tour met maintenant le message en file."),
		styles.green.Render(glyphs.success+" ajouté") + "  " + styles.del.Render(glyphs.failure+" retiré") + "  " + styles.warn.Render(glyphs.bolt+" attente"),
		styles.overlaySel.Render(padTo(truncate("> ligne sélectionnée, en vidéo inverse", width), width)),
		styles.dim.Render(glyphs.notice+" notice") + "  " + styles.red.Render("erreur"),
		themeBarLine(styles),
	}
	for index := range lines {
		lines[index] = truncate(lines[index], width)
	}
	return lines
}

func themeBarLine(styles uiStyles) string {
	parts := []string{styles.dim.Render("ctx")}
	for _, ratio := range []float64{0.41, 0.85, 0.97} {
		parts = append(parts, themeBar(styles, ratio))
	}
	return strings.Join(parts, "  ")
}

func themeBar(styles uiStyles, ratio float64) string {
	filled := int(math.Round(ratio * themeGaugeCells))
	fill := styles.accent
	switch {
	case ratio >= ctxAlarm:
		fill = styles.red
	case ratio >= ctxWarn:
		fill = styles.warn
	}
	percent := int(math.Round(ratio * percentScale))
	return fill.Render(strings.Repeat(styles.glyphs.gaugeFull, filled)) +
		styles.dim.Render(strings.Repeat(styles.glyphs.gaugeEmpty, themeGaugeCells-filled)) +
		styles.dim.Render(fmt.Sprintf(" %d%%", percent))
}

func themeColumns(left, right []string, leftWidth int, divider string, rows int) []string {
	height := max(len(left), len(right))
	lines := make([]string, 0, height)
	for index := range height {
		var leftLine, rightLine string
		if index < len(left) {
			leftLine = left[index]
		}
		if index < len(right) {
			rightLine = right[index]
		}
		lines = append(lines, padTo(leftLine, leftWidth)+divider+rightLine)
	}
	return clipLines(lines, rows)
}

func clipLines(lines []string, limit int) []string {
	if len(lines) > limit {
		return lines[:limit]
	}
	return lines
}
