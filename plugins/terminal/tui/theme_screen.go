package tui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	themeScreenSideBySide = 100
	themeLeftWidth        = 56
	themeLabelWidth       = 16
	themeKeyColumn        = 11
	themeHeadGap          = 2
	themePersonalID       = "perso"
	themePersonalFile     = themePersonalID + themeFileExt
	themePersonalName     = "Mon thème"
	themeApprovalAt       = 4*time.Minute + 12*time.Second
	themeApprovalMinWidth = 30
	themeTokenColumn      = 8
	themeReadSpan         = 100 * time.Millisecond
	themeEditSpan         = 200 * time.Millisecond
	themeBashSpan         = 3100 * time.Millisecond
)

type themeScreen struct {
	themes   []Theme
	paths    map[string]string
	cursor   int
	origin   Theme
	look     renderOptions
	auto     bool
	saved    bool
	install  bool
	problems []themeProblem
}

type themeEditedMsg struct{ err error }

type themeBlock struct {
	lines []string
	gap   bool
}

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

func themeDescription(trans translator, themeID string) (string, bool) {
	switch themeID {
	case themeSombre:
		return trans.t("tui.theme.desc.sombre", nil), true
	case themeClair:
		return trans.t("tui.theme.desc.clair", nil), true
	case themeTerminal:
		return trans.t("tui.theme.desc.terminal", nil), true
	case themeContraste:
		return trans.t("tui.theme.desc.contraste", nil), true
	}
	return "", false
}

func (s *themeScreen) detail(trans translator, themeID string) string {
	if text, ok := themeDescription(trans, themeID); ok {
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
	m.themeScr.saved = false
	m.applyTheme(m.themeScr.preview())
}

func (m *model) handleThemeKey(msg tea.KeyMsg) tea.Cmd {
	screen := m.themeScr
	stack := ctxThemePicker
	if screen.install {
		stack = ctxThemeInstall
	}
	action := m.stackAction(msg, stack)
	if m.overlayNav(action, func(delta int) {
		screen.move(delta)
		m.previewTheme()
	}) {
		return nil
	}
	if run, ok := m.tables().themeScreen[action]; ok {
		return run(m, msg)
	}
	return m.handleThemeAction(action)
}

func themeScreenRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actSelectCancel, cmdKey(func(app *model) tea.Cmd {
			if app.themeScr.install {
				app.themeScr.install = false
				return nil
			}
			app.applyTheme(app.themeScr.origin)
			app.closeTheme()
			return nil
		})),
		bind[keyRun](actSelectAccept, stepKey((*model).commitTheme)),
		bind[keyRun](actThemeEdit, cmdKey((*model).editThemeFile)),
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
	)
}

func (m *model) handleThemeAction(action keyAction) tea.Cmd {
	if run, ok := m.tables().themeLook[action]; ok {
		return run(m)
	}
	return nil
}

func themeLookRuns() (map[keyAction]modelRun, error) {
	return dispatchOf(
		bind[modelRun](actThemeInstall, func(app *model) tea.Cmd {
			app.themeScr.install = !app.themeScr.install
			return nil
		}),
		bind[modelRun](actThemeCopyCommand, func(app *model) tea.Cmd {
			app.copyObel(obelInstallCommand, app.tr.t("tui.theme.copy.command", nil))
			return nil
		}),
		bind[modelRun](actThemeCopyConfig, func(app *model) tea.Cmd {
			app.copyObel(app.obelConfigText(), app.tr.t("tui.theme.copy.config", nil))
			return nil
		}),
		bind[modelRun](actThemeGlyphs, themeTweak(func(s *themeScreen) { s.look.Glyphs = nextOption(glyphModes(), s.look.Glyphs) })),
		bind[modelRun](actThemeBorders, themeTweak(func(s *themeScreen) { s.look.Borders = nextOption(borderStyles(), s.look.Borders) })),
		bind[modelRun](actThemeDensity, themeTweak(func(s *themeScreen) { s.look.Density = nextOption(densities(), s.look.Density) })),
		bind[modelRun](actThemeBold, themeTweak(func(s *themeScreen) { s.look.Bold = !s.look.Bold })),
		bind[modelRun](actThemeItalic, themeTweak(func(s *themeScreen) { s.look.Italic = !s.look.Italic })),
		bind[modelRun](actThemeAuto, themeTweak(func(s *themeScreen) { s.auto = !s.auto })),
	)
}

func themeTweak(change func(*themeScreen)) modelRun {
	return func(app *model) tea.Cmd {
		change(app.themeScr)
		app.previewTheme()
		return nil
	}
}

func (m *model) themeHelpShort() string {
	return joinNonEmpty(
		m.pairHint(actSelectPrevious, actSelectNext, m.tr.t("tui.theme.preview_short", nil), ctxThemePicker),
		m.labelledHint(actSelectAccept, m.tr.t("tui.theme.keep", nil), ctxThemePicker),
		m.labelledHint(actSelectCancel, m.tr.t("tui.theme.back", nil), ctxThemePicker),
		m.labelledHint(actThemeEdit, m.tr.t("tui.theme.edit", nil), ctxThemePicker),
	)
}

func (m *model) commitTheme() {
	screen := m.themeScr
	if !screen.keepable() {
		m.flash = m.tr.t("tui.theme.file_absent", map[string]any{"File": "themes/" + themePersonalFile, tplArgAction: m.labelledHint(actThemeEdit, m.tr.t("tui.theme.create_it", nil), ctxThemePicker)})
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
		m.flash = m.tr.t("tui.theme.not_saved", map[string]any{tplArgReason: localizedReason(m.tr, err)})
		return
	}
	screen.origin = preview
	screen.saved = true
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
		m.flash = m.tr.t("tui.theme.not_opened", map[string]any{tplArgReason: localizedReason(m.tr, err)})
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
		m.flash = m.tr.t("tui.theme.editor_failed", map[string]any{tplArgReason: localizedReason(m.tr, msg.err)})
	}
}

func (m *model) themeScreenView() string {
	top := m.railLines()
	bottom := m.statusZone()
	rows := max(m.height-len(top)-len(bottom), 0)
	lines := make([]string, 0, m.height)
	lines = append(lines, top...)
	lines = append(lines, padLines(m.themeBody(m.innerWidth(), rows), rows)...)
	lines = append(lines, bottom...)
	return m.st.finish(strings.Join(clipLines(lines, m.height), "\n"))
}

func (m *model) themeBody(width, rows int) []string {
	if m.themeScr.install {
		return m.obelInstallLines(width, rows)
	}
	if width >= themeScreenSideBySide {
		left := m.themeControlsFitting(themeLeftWidth, rows)
		right := m.themePreview(width-themeLeftWidth-1, rows)
		return themeColumns(left, right, themeLeftWidth, m.st.dim.Render(m.st.glyphs.divider), rows)
	}
	body := m.themeControlsFitting(width, rows)
	if previewRoom := rows - m.themePreviewMinRows(width) - 1; len(body) > previewRoom {
		if folded := m.themeControlsFitting(width, previewRoom); len(folded) <= previewRoom {
			body = folded
		}
	}
	if room := rows - len(body) - 1; room > 0 {
		if preview := m.themePreview(width, room); len(preview) > 0 {
			body = append(body, "")
			body = append(body, preview...)
		}
	}
	return clipLines(body, rows)
}

type themeFold int

const (
	themeFoldNone themeFold = iota
	themeFoldFontExplain
	themeFoldDetection
	themeFoldHelp
)

func (m *model) themeControlsFitting(width, rows int) []string {
	var lines []string
	for fold := themeFoldNone; fold <= themeFoldHelp; fold++ {
		lines = m.themeControls(width, fold)
		if len(lines) <= rows {
			return lines
		}
	}
	return lines
}

func (m *model) themeControls(width int, fold themeFold) []string {
	styles := m.st
	screen := m.themeScr
	lines := []string{styles.strong.Render(m.tr.t("tui.theme.title", nil)) + styles.dim.Render(" · /theme · "+m.tr.t("tui.theme.live_preview", nil))}
	lines = append(lines, m.themeListLines(width)...)
	if len(screen.problems) > 0 {
		lines = append(lines, styles.dim.Render(screen.problems[0].text(m.tr)))
	}
	lines = append(lines, "")
	if fold >= themeFoldHelp {
		lines = append(lines, styles.dim.Render(m.themeHelpShort()))
	} else {
		lines = append(lines,
			styles.dim.Render(joinNonEmpty(
				m.pairHint(actSelectPrevious, actSelectNext, m.tr.t("tui.theme.live_preview", nil), ctxThemePicker),
				m.labelledHint(actSelectAccept, m.tr.t("tui.theme.keep", nil), ctxThemePicker),
				m.labelledHint(actSelectCancel, m.tr.t("tui.theme.back", nil), ctxThemePicker),
			)),
			styles.dim.Render(m.labelledHint(actOpenTheme, m.tr.t("tui.theme.anywhere", nil), chatKeyStack()...)),
			styles.dim.Render(m.labelledHint(actThemeEdit, m.tr.t("tui.theme.edit_file", map[string]any{"File": "themes/" + themePersonalFile}), ctxThemePicker)),
		)
	}
	lines = append(lines,
		"",
		themeSectionHead(styles, m.tr.t("tui.theme.render", nil), m.tr.t("tui.theme.in_theme", nil), width),
		themeOptionRow(styles, m.tr, m.tr.t("tui.theme.row.glyphs", nil), glyphModes(), screen.look.Glyphs, glyphLabel, "g"),
		themeOptionRow(styles, m.tr, m.tr.t("tui.theme.row.borders", nil), borderStyles(), screen.look.Borders, borderLabel, "b"),
		themeOptionRow(styles, m.tr, m.tr.t("tui.theme.row.density", nil), []density{densityCompact, densityNormal}, screen.look.Density, densityLabel, "d"),
		themeFlagRow(styles, m.tr, screen.look.Bold, screen.look.Italic),
		themeFontRow(styles, m.tr, m.obelFont),
	)
	if fold < themeFoldFontExplain {
		lines = append(lines,
			themeFontExplainRow(styles, m.tr.t("tui.theme.font.explain_one", nil)),
			themeFontExplainRow(styles, m.tr.t("tui.theme.font.explain_two", nil)),
		)
	}
	if fold < themeFoldDetection {
		lines = append(lines,
			"",
			themeSectionHead(styles, m.tr.t("tui.theme.detection", nil), "", width),
			m.themeBackgroundRow(),
			themeAutoRow(styles, m.tr, screen.auto),
			themeNoColorRow(styles, m.tr),
		)
	}
	for index := range lines {
		lines[index] = cutText(lines[index], width)
	}
	return lines
}

func (m *model) themeBackgroundRow() string {
	label := m.st.text.Render(padTo(m.tr.t("tui.theme.row.background", nil), themeKeyColumn))
	if m.themeBackground == "" {
		return "   " + label + m.st.text.Render(m.tr.t("tui.theme.background.unknown", nil)) +
			m.st.dim.Render(" · "+m.tr.t("tui.theme.background.not_read", nil))
	}
	return "   " + label + m.st.text.Render(backgroundName(m.tr, m.themeBackground)) +
		m.st.dim.Render(" · "+m.tr.t("tui.theme.background.read_osc11", nil))
}

func backgroundName(trans translator, code string) string {
	if code == backgroundDark {
		return trans.t("tui.theme.background.dark", nil)
	}
	return trans.t("tui.theme.background.light", nil)
}

func themeNoColorRow(styles uiStyles, trans translator) string {
	return "   " + styles.dim.Render(trans.t("tui.theme.no_color", nil))
}

func themeFontExplainRow(styles uiStyles, text string) string {
	return "   " + styles.dim.Render(text)
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
	head, mark := "   ", glyphs.open
	if selected {
		head = " " + glyphs.prompt + " "
	}
	if current {
		mark = glyphs.success
	}
	label := padTo(theme.displayName(m.tr), themeLabelWidth)
	detail := m.themeScr.detail(m.tr, theme.ID)
	if selected {
		return styles.overlaySel.Render(padTo(cutText(head+mark+" "+label+"  "+detail, width), width))
	}
	markStyle := styles.dim
	if current {
		markStyle = styles.green.Bold(true)
	}
	return padTo(cutText(styles.dim.Render(head)+markStyle.Render(mark)+" "+styles.text.Render(label)+styles.dim.Render(detail), width), width)
}

func themeSectionHead(styles uiStyles, title, extra string, width int) string {
	used := lipgloss.Width(title) + themeHeadGap
	head := styles.dim.Render(strings.ToUpper(title))
	if extra != "" {
		used += lipgloss.Width(extra) + 1
		head += styles.dim.Render(" " + extra)
	}
	rule := strings.Repeat(styles.glyphs.ruleChar, max(width-used, 1))
	return head + " " + styles.dim.Render(rule)
}

func themeOptionRow[Choice comparable](styles uiStyles, trans translator, label string, choices []Choice, current Choice, name func(translator, Choice) string, key string) string {
	var row strings.Builder
	row.WriteString("   " + padTo(label, themeKeyColumn))
	for _, choice := range choices {
		if choice == current {
			row.WriteString(styles.chip.Render(name(trans, choice)))
		} else {
			row.WriteString(styles.dim.Render(" " + name(trans, choice) + " "))
		}
		row.WriteString(" ")
	}
	row.WriteString(styles.dim.Render(key))
	return row.String()
}

func themeFlagRow(styles uiStyles, trans translator, bold, italic bool) string {
	return "   " + padTo(trans.t("tui.theme.row.text", nil), themeKeyColumn) + themeFlag(styles, bold, trans.t("tui.theme.flag.bold", nil)) + "  " +
		themeFlag(styles, italic, trans.t("tui.theme.flag.italic", nil)) + "  " + styles.dim.Render("w i")
}

func themeAutoRow(styles uiStyles, trans translator, auto bool) string {
	return "   " + themeFlag(styles, auto, trans.t("tui.theme.flag.auto", nil)) +
		styles.dim.Render(" : "+trans.t("tui.theme.auto_hint", nil)) + "  " + styles.dim.Render("a")
}

func themeFlag(styles uiStyles, on bool, label string) string {
	if on {
		return styles.text.Render(styles.glyphs.ticked + " " + label)
	}
	return styles.dim.Render(styles.glyphs.unticked + " " + label)
}

func glyphLabel(trans translator, mode glyphMode) string {
	if mode == glyphsNerd {
		return trans.t("tui.theme.glyph.nerd", nil)
	}
	return mode.String()
}

func borderLabel(trans translator, style borderStyle) string {
	switch style {
	case borderSquare:
		return trans.t("tui.theme.border.square", nil)
	case borderNone:
		return trans.t("tui.theme.border.none", nil)
	case borderRounded:
		return trans.t("tui.theme.border.rounded", nil)
	}
	return trans.t("tui.theme.border.rounded", nil)
}

func densityLabel(trans translator, level density) string {
	if level == densityCompact {
		return trans.t("tui.theme.density.compact", nil)
	}
	return trans.t("tui.theme.density.normal", nil)
}

const minPreviewBlocks = 2

func (m *model) themePreviewMinRows(width int) int {
	rows := 0
	for _, block := range m.themePreviewBlocks(width)[:minPreviewBlocks] {
		rows += len(block.lines)
	}
	return rows
}

func (m *model) themePreview(width, rows int) []string {
	return fitThemeBlocks(m.themePreviewBlocks(width), width, rows)
}

func fitThemeBlocks(blocks []themeBlock, width, rows int) []string {
	var out []string
	fitted := 0
	for _, block := range blocks {
		if len(block.lines) == 0 {
			continue
		}
		var lines []string
		if block.gap && len(out) > 0 {
			lines = append(lines, "")
		}
		for _, line := range block.lines {
			lines = append(lines, cutText(line, width))
		}
		if len(out)+len(lines) > rows {
			break
		}
		out = append(out, lines...)
		fitted++
	}
	if fitted < minPreviewBlocks {
		return nil
	}
	return out
}

func (m *model) themePreviewBlocks(width int) []themeBlock {
	styles := m.st
	screen := m.themeScr
	shown := screen.preview()
	kept := m.tr.t("tui.theme.not_kept", nil)
	if shown.ID == screen.origin.ID {
		kept = m.tr.t("tui.theme.kept", nil)
	}
	return []themeBlock{
		{lines: []string{styles.dim.Render(m.tr.t("tui.theme.preview_label", map[string]any{tplArgName: shown.displayName(m.tr), "State": kept}))}},
		{lines: m.railLines()},
		{lines: []string{m.themeFileLine(width)}},
		{lines: m.themeToolLines(width)},
		{lines: m.themeDiffLines(width)},
		{lines: m.themeApprovalLines(width)},
		{lines: m.themeLegendLines()},
		{lines: []string{themeGaugeLine(styles, m.tr)}},
		{lines: []string{styles.overlaySel.Render(padTo(cutText("> "+m.tr.t("tui.theme.selected_line", nil), width), width))}},
		{lines: m.themeModeLines(width)},
		{gap: true, lines: m.themeTerminalLines()},
		{gap: true, lines: m.themePersonalLines(shown)},
	}
}

func (m *model) themeFileLine(width int) string {
	styles := m.st
	glyphs := styles.glyphs
	if glyphs.fileGo == "" {
		return cutText(styles.text.Render("tui/theme.go  tui/rail.go  README.md")+"  "+styles.dim.Render("feat/tui-v2"), width)
	}
	line := styles.accent.Render(glyphs.fileGo) + " " + styles.text.Render("theme.go") + "  " +
		styles.accent.Render(glyphs.fileGo) + " " + styles.text.Render("rail.go") + "  " +
		styles.dim.Render(glyphs.fileDoc) + " " + styles.text.Render("README.md") + "  " +
		styles.violet.Render(glyphs.branch) + " " + styles.dim.Render("feat/tui-v2")
	return cutText(line, width)
}

func (m *model) themeToolLines(width int) []string {
	display := m.displayMode()
	return []string{
		renderTool(entry{text: "Read tui/theme.go", span: themeReadSpan}, width, display),
		renderTool(entry{text: "Edit tui/theme.go +12 −4", span: themeEditSpan}, width, display),
		renderTool(entry{text: "Bash go vet ./...", span: themeBashSpan, failed: true}, width, display),
	}
}

func (m *model) themeDiffLines(width int) []string {
	return diffRows([]string{
		"-\tcolText   = \"#c6d0e0\"",
		"+\ttext  = theme.Text",
		"+\taccent = theme.Accent",
	}, width, m.displayMode())
}

func (m *model) themeApprovalLines(width int) []string {
	box := m.st.boxes(m.caps.Boxes)
	boxWidth := min(width, approvalMaxWidth)
	if boxWidth < themeApprovalMinWidth {
		return nil
	}
	now := time.Now()
	prompt := approvalPrompt{tool: "Bash", command: "git push origin feat/tui-v2", deadline: now.Add(themeApprovalAt)}
	inner := boxWidth - approvalFrameCols
	footer := box.bl + box.h + " " + m.st.dim.Render(joinNonEmpty(m.pairHint(actSelectPrevious, actSelectNext, m.tr.t("tui.theme.approval.choose", nil), ctxApproval), m.labelledHint(actSelectAccept, m.tr.t("tui.theme.approval.accept", nil), ctxApproval), m.labelledHint(actApprovalAccelerate, m.tr.t("tui.theme.approval.speed_up", nil), ctxApproval))) + " "
	return []string{
		m.approvalTop(prompt, boxWidth, box, now),
		approvalRow(box, m.approvalCommand(prompt, inner), inner),
		m.approvalBorder(footer, box.br, box.h, boxWidth),
	}
}

func (m *model) themeLegendLines() []string {
	styles := m.st
	glyphs := styles.glyphs
	return []string{
		styles.text.Render(m.tr.t("tui.theme.legend.queue", map[string]any{tplArgKey: m.hintIn(actSubmit, chatKeyStack()...)})),
		styles.green.Render(glyphs.success+" "+m.tr.shared("cli.manage.state.added")) + "  " + styles.del.Render(glyphs.failure+" "+m.tr.shared("cli.manage.state.removed")) + "  " + styles.warn.Render(glyphs.bolt+" "+m.tr.t("tui.theme.legend.waiting", nil)),
		styles.dim.Render(glyphs.notice+" "+m.tr.t("tui.theme.legend.notice", nil)) + "  " + styles.red.Render(m.tr.t("tui.theme.legend.error", nil)),
	}
}

func themeGaugeLine(styles uiStyles, trans translator) string {
	parts := []string{styles.dim.Render(trans.t("tui.theme.gauge_label", nil))}
	for _, ratio := range []float64{0.41, 0.85, 0.97} {
		percent := int(math.Round(ratio * percentScale))
		parts = append(parts, contextGaugeCells(ratio, barContextCells, styles)+styles.dim.Render(fmt.Sprintf(" %d%%", percent)))
	}
	return strings.Join(parts, "  ")
}

func (m *model) themeModeLines(width int) []string {
	strip := m.modeStripView(&tab{}, width)
	return append(strip, m.st.spinner.Render(m.spinFrame())+m.st.dim.Render(" "+m.tr.t("tui.common.working", nil)))
}

func (m *model) themeTerminalLines() []string {
	styles := m.st
	screen := m.themeScr
	themeID, auto := m.themeCfg.themeID(), m.themeCfg.autoTheme()
	saved := ""
	if screen.saved {
		themeID, auto = screen.origin.ID, screen.auto
		saved = styles.dim.Render("   " + m.tr.t("tui.theme.saved_now", nil))
	}
	return []string{
		styles.text.Bold(true).Render(m.terminalConfigPath()),
		styles.dim.Render("[terminal]"),
		styles.text.Render("theme      = ") + styles.accent.Bold(true).Render(quoteTheme(themeID)) + saved,
		styles.text.Render("auto_theme = " + strconv.FormatBool(auto)),
	}
}

func (m *model) themePersonalLines(shown Theme) []string {
	styles := m.st
	render := shown.Render
	tokens := [][2]string{
		{"name", quoteTheme(themePersonalName)},
		{"base", quoteTheme(m.themeScr.baseID())},
		{"accent", quoteTheme(string(shown.Colors.Accent))},
		{"add", quoteTheme(string(shown.Colors.Add))},
		{"del", quoteTheme(string(shown.Colors.Del))},
		{"wait", quoteTheme(string(shown.Colors.Wait))},
		{"glyphs", quoteTheme(render.Glyphs.String())},
		{"borders", quoteTheme(render.Borders.String())},
		{"density", quoteTheme(render.Density.String())},
		{"bold", strconv.FormatBool(render.Bold)},
		{"italic", strconv.FormatBool(render.Italic)},
	}
	lines := []string{styles.text.Bold(true).Render(filepath.Join(themesDirFor(m.themeDir), themePersonalFile))}
	for _, token := range tokens {
		lines = append(lines, "   "+styles.text.Render(padTo(token[0], themeTokenColumn))+styles.dim.Render("= ")+styles.text.Render(token[1]))
	}
	return append(lines,
		styles.dim.Render(m.tr.t("tui.theme.personal_inherits", nil)),
		styles.dim.Render(m.tr.t("tui.theme.personal_others", nil)),
	)
}

func quoteTheme(value string) string {
	return "\"" + value + "\""
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
