package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	themeScreenSideBySide = 100
	themeListWidth        = 34
	themePersonalSuffix   = "-perso"
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
	for index, th := range s.themes {
		if th.ID == selected {
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
	th := s.themes[s.cursor]
	th.Render = s.look
	return th
}

func (s *themeScreen) move(delta int) {
	s.cursor = min(max(s.cursor+delta, 0), len(s.themes)-1)
}

func glyphCycle() []string {
	return []string{glyphsUnicode, glyphsNerd, glyphsASCII}
}

func borderCycle() []string {
	return []string{bordersRounded, bordersSquare, bordersNone}
}

func densityCycle() []string {
	return []string{densityNormal, densityCompact}
}

func nextOf(values []string, current string) string {
	for index, value := range values {
		if value == current {
			return values[(index+1)%len(values)]
		}
	}
	return values[0]
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
	s := m.themeScr
	if m.overlayNav(msg, func(delta int) {
		s.move(delta)
		m.previewTheme()
	}) {
		return nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.applyTheme(s.origin)
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
	s := m.themeScr
	switch key {
	case "g":
		s.look.Glyphs = nextOf(glyphCycle(), s.look.Glyphs)
	case "b":
		s.look.Borders = nextOf(borderCycle(), s.look.Borders)
	case "d":
		s.look.Density = nextOf(densityCycle(), s.look.Density)
	case "w":
		s.look.Bold = !s.look.Bold
	case "i":
		s.look.Italic = !s.look.Italic
	case "a":
		s.auto = !s.auto
	case "e":
		return m.editThemeFile()
	default:
		return nil
	}
	m.previewTheme()
	return nil
}

func (m *model) commitTheme() {
	s := m.themeScr
	preview := s.preview()
	base := s.themes[s.cursor]
	id, auto := preview.ID, s.auto
	section := terminalSection{Theme: &id, AutoTheme: &auto}
	section.Glyphs = overrideString(preview.Render.Glyphs, base.Render.Glyphs)
	section.Borders = overrideString(preview.Render.Borders, base.Render.Borders)
	section.Density = overrideString(preview.Render.Density, base.Render.Density)
	section.Bold = overrideBool(preview.Render.Bold, base.Render.Bold)
	section.Italic = overrideBool(preview.Render.Italic, base.Render.Italic)
	if err := m.saveThemeConfig(section); err != nil {
		m.flash = "thème non enregistré : " + err.Error()
	}
	m.closeTheme()
}

func overrideString(value, inherited string) *string {
	if value == inherited {
		return nil
	}
	return &value
}

func overrideBool(value, inherited bool) *bool {
	if value == inherited {
		return nil
	}
	return &value
}

func (m *model) editThemeFile() tea.Cmd {
	editor := strings.Fields(os.Getenv("EDITOR"))
	if len(editor) == 0 {
		m.flash = "EDITOR non défini"
		return nil
	}
	path, err := m.themeFileForEdit()
	if err != nil {
		m.flash = "thème non ouvert : " + err.Error()
		return nil
	}
	command := exec.Command(editor[0], append(editor[1:], path)...)
	return tea.ExecProcess(command, func(err error) tea.Msg { return themeEditedMsg{err: err} })
}

func (m *model) themeFileForEdit() (string, error) {
	id := m.themeScr.preview().ID
	if path, ok := m.themeCat.paths[id]; ok {
		return path, nil
	}
	dir := themesDirFor(m.themeDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, id+themePersonalSuffix+themeFileExt)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	body := fmt.Sprintf("name = %q\nbase = %q\n", "Mon thème", id)
	return path, os.WriteFile(path, []byte(body), 0o600)
}

func (m *model) handleThemeEdited(msg themeEditedMsg) {
	m.reloadThemes()
	if msg.err != nil {
		m.flash = "éditeur : " + msg.err.Error()
	}
}

func (m *model) themeView() string {
	s := m.themeScr
	width := m.innerWidth()
	header := m.st.accent.Render("Thème") + "\n" + wrapWith(m.st.dim, s.optionsLine(), width)
	list := floatingList(s.rows(), s.cursor, s.footer(), min(width, themeListWidth), m.st)
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
	for _, th := range s.themes {
		rows = append(rows, overlayRow{label: th.Label})
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
	st := m.st
	glyphs := st.glyphs
	boxes := st.boxes(true)
	inner := max(min(width-4, 56), 20)
	body := func(text string) string {
		return st.dim.Render(boxes.v) + " " + padTo(truncate(text, inner), inner) + " " + st.dim.Render(boxes.v)
	}
	frame := []string{
		st.accent.Render(boxes.tl + strings.Repeat(boxes.h, inner+2) + boxes.tr),
		body(st.text.Render("Texte de réponse, en couleur normale")),
		body(st.green.Render(glyphs.success+" ajouté") + "  " + st.del.Render(glyphs.failure+" retiré") + "  " + st.warn.Render(glyphs.bolt+" attente")),
		body(st.overlaySel.Render(" sélection ") + "  " + st.dim.Render(glyphs.notice+" notice") + "  " + st.red.Render("erreur")),
		body(gauge(0.4, st.green, st.dim, glyphs)),
		st.accent.Render(boxes.bl + strings.Repeat(boxes.h, inner+2) + boxes.br),
	}
	title := st.accent.Render(glyphs.agentTitle()) + st.dim.Render(" aperçu : "+m.themeScr.preview().Label)
	return append([]string{title, ""}, frame...)
}
