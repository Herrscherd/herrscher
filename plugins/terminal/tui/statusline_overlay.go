package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	barLabelWidth    = 20
	barListWidth     = 54
	barColumnDivider = "│"
	barMinPaneWidth  = 40
	barPaneIndent    = "   "
	barScriptPath    = "~/.config/herrscher/statusline.sh"
	barConfigPath    = "~/.config/herrscher/terminal.toml"
	barScriptNote    = " · " + barScriptPath
	barBottomRows    = 3
	barHeaderRows    = 4
	barChromeRows    = 11
)

func barScriptSample(trans translator) []string {
	return []string{
		"#!/bin/sh",
		`jq -r '[.mode, .model, .git.branch,`,
		`  "\(.context.percent)%", "$\(.cost.usd)",`,
		"  (if .queue > 0 then \"\\(.queue) " + trans.t("tui.statusline.sample_queue", nil) + "\" else empty end)]",
		`  | join(" │ ")'`,
	}
}

func barJSONSample() []string {
	return []string{
		`{ "session": "tui-v2", "vendor": "claude", "model": "opus",`,
		`  "mode": "manuel", "gateway": "terminal",`,
		`  "git": { "repo": "herrscher", "branch": "feat/tui-v2",`,
		`           "worktree": ".worktrees/tui-v2" },`,
		`  "context": { "used": 82000, "percent": 41 },`,
		`  "cost": { "usd": 1.42 }, "queue": 2, "background": 1,`,
		`  "turn": { "busy": true, "secs": 72 } }`,
	}
}

func (m *model) openStatusline() tea.Cmd {
	m.bar.open = true
	m.bar.cursor = 0
	m.relayout()
	return m.armStatusline()
}

func (m *model) closeStatusline() {
	m.bar.open = false
	if err := saveStatuslineChoice(m.bar.file, &m.bar); err != nil {
		m.flash = m.tr.t("tui.statusline.not_written", map[string]any{tplArgReason: localizedReason(m.tr, err)})
	}
	m.relayout()
}

func (m *model) handleStatuslineKey(msg tea.KeyMsg) tea.Cmd {
	action := m.stackAction(msg, ctxStatusline)
	if run, ok := m.tables().statusline[action]; ok {
		return run(m, msg)
	}
	if m.bar.scripted() {
		return nil
	}
	if m.overlayNav(action, m.moveStatuslineCursor) {
		return nil
	}
	if run, ok := m.tables().statuslineField[action]; ok {
		return run(m, msg)
	}
	return nil
}

func statuslineRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actStatuslineClose, stepKey((*model).closeStatusline)),
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
		bind[keyRun](actStatuslineSource, cmdKey((*model).switchStatuslineSource)),
	)
}

func statuslineFieldRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actStatuslineUp, stepKey(func(app *model) { app.moveStatuslineField(-1) })),
		bind[keyRun](actStatuslineDown, stepKey(func(app *model) { app.moveStatuslineField(1) })),
		bind[keyRun](actStatuslineToggle, stepKey((*model).toggleStatuslineField)),
	)
}

func (m *model) moveStatuslineCursor(delta int) {
	last := len(m.bar.fieldIDs()) - 1
	m.bar.cursor = max(0, min(m.bar.cursor+delta, last))
}

func (m *model) moveStatuslineField(delta int) {
	if m.bar.moveField(m.bar.cursor, delta) {
		m.moveStatuslineCursor(delta)
	}
}

func (m *model) toggleStatuslineField() {
	ids := m.bar.fieldIDs()
	if m.bar.cursor < len(ids) {
		m.bar.toggle(ids[m.bar.cursor])
	}
}

func (m *model) switchStatuslineSource() tea.Cmd {
	m.bar.script.line = ""
	m.bar.script.failed = false
	if m.bar.scripted() {
		m.bar.config.Source = barSourceBuiltin
		return nil
	}
	m.bar.config.Source = barSourceScript
	return m.armStatusline()
}

func (m *model) statuslineView() string {
	width := m.innerWidth()
	paneWidth := width - barListWidth - lipgloss.Width(barColumnDivider)
	var top []string
	if paneWidth >= barMinPaneWidth {
		controls := m.statuslineControls(barListWidth)
		pane := m.statuslinePane(paneWidth, m.statuslinePaneBudget(0))
		top = joinStatuslineColumns(controls, pane, barListWidth, m.st.dim.Render(barColumnDivider))
	} else {
		top = m.statuslineControls(width)
		top = append(top, m.statuslinePane(width, m.statuslinePaneBudget(len(top)))...)
	}
	bottom := []string{
		m.st.rule(m.st.dim, width),
		m.statuslinePreviewCaption(width),
		statusIndent + m.statusLeftPreview(),
	}
	return strings.Join(append(top, bottom...), "\n")
}

func (m *model) statuslinePaneBudget(used int) int {
	if m.height <= 0 {
		return unboundedRows
	}
	return m.height - used - barBottomRows - barChromeRows
}

func joinStatuslineColumns(left, right []string, leftWidth int, divider string) []string {
	rows := max(len(left), len(right))
	out := make([]string, 0, rows)
	for index := range rows {
		cell := padTo(cutText(lineAt(left, index), leftWidth), leftWidth)
		out = append(out, cell+divider+lineAt(right, index))
	}
	return out
}

func lineAt(lines []string, index int) string {
	if index < len(lines) {
		return lines[index]
	}
	return ""
}

func (m *model) statuslineControls(width int) []string {
	lines := []string{
		statusIndent + m.st.accent.Render(m.tr.t("tui.statusline.title", nil)) + m.st.dim.Render(" · /statusline"),
		m.statuslineSourceLine(),
		"",
	}
	lines = append(lines, m.statuslineRows(width)...)
	footer := m.statuslineFooter()
	if m.bar.scripted() {
		footer = m.tr.t("tui.statusline.script_footer", nil)
	}
	return append(lines, m.st.dim.Render(cutText(statusIndent+footer, width)))
}

func (m *model) statuslineListRows() int {
	rows := m.overlayRows()
	if m.height > 0 {
		rows = min(rows, m.height-barChromeRows-barBottomRows-barHeaderRows)
	}
	return max(1, rows)
}

func (m *model) statuslineRows(width int) []string {
	ids := m.bar.fieldIDs()
	start, end := paletteWindow(len(ids), m.bar.cursor, m.statuslineListRows())
	rows := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		selected := index == m.bar.cursor && !m.bar.scripted()
		rows = append(rows, m.statuslineRow(ids[index], selected, width))
	}
	return rows
}

func (m *model) statuslineRow(fieldID string, selected bool, width int) string {
	spec := barFieldSpecOf(fieldID)
	shown := m.bar.isShown(fieldID)
	head := "  "
	if selected {
		head = m.st.glyphs.cursor + " "
	}
	mark := m.st.glyphs.unticked
	if shown {
		mark = m.st.glyphs.ticked
	}
	label := padTo(spec.labelWith(m.tr, m.hintIn(actCycleMode, chatKeyStack()...)), barLabelWidth)
	if selected {
		plain := statusIndent + head + mark + " " + label + " " + spec.note(m.tr)
		return m.st.overlaySel.Render(padTo(cutText(plain, width), width))
	}
	markStyle, labelStyle := m.st.dim, m.st.dim
	if m.bar.scripted() {
		markStyle, labelStyle = m.st.scrollback, m.st.scrollback
	}
	if shown {
		markStyle = m.st.accent
		if !m.bar.scripted() {
			labelStyle = m.st.text
		}
	}
	row := statusIndent + head + markStyle.Render(mark+" ") + labelStyle.Render(label+" ") + m.st.dim.Render(spec.note(m.tr))
	return padTo(cutText(row, width), width)
}

func (m *model) statuslineSourceLine() string {
	builtin, script := m.st.dim.Render(" "+m.tr.t("tui.statusline.builtin", nil)+" "), m.st.dim.Render(" "+m.tr.t("tui.statusline.script", nil)+" ")
	if m.bar.scripted() {
		script = m.st.chip.Render(m.tr.t("tui.statusline.script", nil))
	} else {
		builtin = m.st.chip.Render(m.tr.t("tui.statusline.builtin", nil))
	}
	return statusIndent + m.st.dim.Render(m.tr.t("tui.statusline.source", nil)+"  ") + builtin + "  " + script + m.st.dim.Render("   "+m.tr.t("tui.statusline.toggle", nil))
}

func (m *model) statuslinePane(width, budget int) []string {
	headStyle, bodyStyle := m.st.dim.Bold(true), m.st.dim
	if m.bar.scripted() {
		headStyle, bodyStyle = m.st.accent.Bold(true), m.st.text
	}
	blocks := [][]string{
		append([]string{m.paneHeading(headStyle, m.tr.t("tui.statusline.script_heading", nil), barScriptNote)}, paneBody(bodyStyle, barScriptSample(m.tr))...),
		append([]string{"", m.paneHeading(m.st.dim.Bold(true), m.tr.t("tui.statusline.json_caption", nil), m.tr.t("tui.statusline.json_note", nil))}, paneBody(m.st.dim, barJSONSample())...),
		append([]string{"", m.paneHeading(m.st.dim.Bold(true), barConfigPath, "")}, m.statuslineTOMLLines()...),
	}
	lines := foldPaneBlocks(blocks, budget)
	for index, line := range lines {
		lines[index] = cutText(line, width)
	}
	return lines
}

func foldPaneBlocks(blocks [][]string, budget int) []string {
	var lines []string
	for _, block := range blocks {
		if len(lines)+len(block) > budget {
			break
		}
		lines = append(lines, block...)
	}
	return lines
}

func (m *model) paneHeading(style lipgloss.Style, title, note string) string {
	return " " + style.Render(strings.ToUpper(title)) + m.st.dim.Render(note)
}

func paneBody(style lipgloss.Style, lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, style.Render(barPaneIndent+line))
	}
	return out
}

func (m *model) statuslineTOMLLines() []string {
	source := barSourceBuiltin
	sourceStyle := m.st.dim
	if m.bar.scripted() {
		source = barSourceScript
		sourceStyle = m.st.green
	}
	return []string{
		m.st.dim.Render(barPaneIndent + "[statusline]"),
		sourceStyle.Render(barPaneIndent + `source  = "` + source + `"`),
		m.st.dim.Render(barPaneIndent + `command = "` + barScriptPath + `"`),
		m.st.dim.Render(barPaneIndent + `refresh = "300ms"`),
	}
}

func (m *model) statuslinePreviewCaption(width int) string {
	captions := statuslinePreviewCaptions(m.tr)
	for _, caption := range captions {
		if lipgloss.Width(statusIndent+caption) <= width {
			return statusIndent + m.st.dim.Render(caption)
		}
	}
	last := captions[len(captions)-1]
	return m.st.dim.Render(cutText(statusIndent+last, width))
}

func statuslinePreviewCaptions(trans translator) []string {
	return []string{
		trans.t("tui.statusline.preview_next_line", nil),
		trans.t("tui.statusline.preview_bar", nil),
		trans.t("tui.statusline.preview_live", nil),
	}
}

func (m *model) statusLeftPreview() string {
	target := m.tabs[m.active]
	if target == nil {
		return ""
	}
	return m.statusBar(target, m.innerWidth()-lipgloss.Width(statusIndent))
}

func (m *model) statuslineFooter() string {
	return joinNonEmpty(
		m.pairHint(actSelectPrevious, actSelectNext, m.tr.t("tui.statusline.choose", nil), ctxStatusline),
		m.labelledHint(actStatuslineToggle, m.tr.t("tui.statusline.check", nil), ctxStatusline),
		m.tr.t("tui.statusline.order", nil),
	)
}
