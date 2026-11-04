package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	themeScreenSideBySide = 100
	themeListWidth        = 34
	themePersonalSuffix   = "-perso"
	themePersonalName     = "Mon thème"
	themePreviewMargin    = 4
	themePreviewMaxInner  = 56
	themePreviewMinInner  = 20
	themePreviewFrameEdge = 2
	themePreviewGauge     = 0.4
)

type themeScreen struct {
	themes   []Theme
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
	s.themes = catalog.themes
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
	return tea.ExecProcess(editorCommand(path), func(err error) tea.Msg { return themeEditedMsg{err: err} })
}

func (m *model) themeFileForEdit() (string, error) {
	themeName := m.themeScr.preview().ID
	if path, ok := m.themeCat.paths[themeName]; ok {
		return path, nil
	}
	dir := themesDirFor(m.themeDir)
	if err := os.MkdirAll(dir, configDirMode); err != nil {
		return "", err
	}
	path := filepath.Join(dir, themeName+themePersonalSuffix+themeFileExt)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	body := fmt.Sprintf("name = %q\nbase = %q\n", themePersonalName, themeName)
	return path, os.WriteFile(path, []byte(body), privateFileMode)
}

func (m *model) handleThemeEdited(msg themeEditedMsg) {
	m.reloadThemes()
	if msg.err != nil {
		m.flash = "éditeur : " + msg.err.Error()
	}
}

func (m *model) themeView() string {
	screen := m.themeScr
	width := m.innerWidth()
	header := m.st.accent.Render("Thème") + "\n" + wrapWith(m.st.dim, screen.optionsLine(), width)
	list := floatingList(screen.rows(), screen.cursor, screen.footer(), min(width, themeListWidth), m.st)
	preview := m.themePreview(width)
	if width < themeScreenSideBySide {
		return strings.Join(append([]string{header, list, ""}, preview...), "\n")
	}
	left := lipgloss.NewStyle().Width(themeListWidth).Render(list)
	return header + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Join(preview, "\n"))
}

func (s *themeScreen) optionsLine() string {
	return fmt.Sprintf("g glyphes %s · b bordures %s · d densité %s · w gras %s · i italique %s · a auto %s · e éditer · ⏎ valider · échap annuler",
		s.look.Glyphs, s.look.Borders, s.look.Density, onOff(s.look.Bold), onOff(s.look.Italic), onOff(s.auto))
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func (s *themeScreen) rows() []overlayRow {
	rows := make([]overlayRow, 0, len(s.themes))
	for index := range s.themes {
		rows = append(rows, overlayRow{label: s.themes[index].Label})
	}
	return rows
}

func (s *themeScreen) footer() string {
	if len(s.problems) == 0 {
		return ""
	}
	return "thème ignoré : " + s.problems[0]
}

func (m *model) themePreview(width int) []string {
	styles := m.st
	glyphs := styles.glyphs
	boxes := styles.boxes(true)
	inner := max(min(width-themePreviewMargin, themePreviewMaxInner), themePreviewMinInner)
	frameLine := strings.Repeat(boxes.h, inner+themePreviewFrameEdge)
	body := func(text string) string {
		return styles.dim.Render(boxes.v) + " " + padTo(truncate(text, inner), inner) + " " + styles.dim.Render(boxes.v)
	}
	frame := []string{
		styles.accent.Render(boxes.tl + frameLine + boxes.tr),
		body(styles.text.Render("Texte de réponse, en couleur normale")),
		body(styles.green.Render(glyphs.success+" ajouté") + "  " + styles.del.Render(glyphs.failure+" retiré") + "  " + styles.warn.Render(glyphs.bolt+" attente")),
		body(styles.overlaySel.Render(" sélection ") + "  " + styles.dim.Render(glyphs.notice+" notice") + "  " + styles.red.Render("erreur")),
		body(gauge(themePreviewGauge, styles.green, styles.dim, glyphs)),
		styles.accent.Render(boxes.bl + frameLine + boxes.br),
	}
	title := styles.accent.Render(glyphs.agentTitle()) + styles.dim.Render(" aperçu : "+m.themeScr.preview().Label)
	return append([]string{title, ""}, frame...)
}
