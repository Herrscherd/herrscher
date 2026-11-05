package tui

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	composerChrome     = 4
	composerBorderCols = 2
	edgeSpacing        = 3
)

const composerBoxMinWidth = 60

func (m *model) composerBoxed() bool {
	return m.innerWidth() >= composerBoxMinWidth
}

func (m *model) composerTextWidth() int {
	if !m.composerBoxed() {
		return m.innerWidth()
	}
	return m.innerWidth() - composerChrome
}

const (
	composerPrompt  = "> "
	ellipsis        = "…"
	worktreesMarker = "/.worktrees/"
)

func promptAt(prompt string) func(int) string {
	indent := strings.Repeat(" ", lipgloss.Width(prompt))
	return func(line int) string {
		if line == 0 {
			return prompt
		}
		return indent
	}
}

func (m *model) promptGlyph() string {
	if m.isShellInput() {
		return m.st.glyphs.shellPrompt + " "
	}
	return m.st.glyphs.prompt + " "
}

func (m *model) syncComposer() {
	m.input.Placeholder = m.placeholderText()
	m.syncPrompt()
}

func (m *model) syncPrompt() {
	prompt := m.promptGlyph()
	m.input.SetPromptFunc(lipgloss.Width(prompt), promptAt(prompt))
	m.input.FocusedStyle.Prompt = m.st.accent.Bold(true)
	m.input.BlurredStyle.Prompt = m.st.accent.Bold(true)
	m.input.FocusedStyle.Text = m.st.text
	m.input.BlurredStyle.Text = m.st.text
	if m.isShellInput() {
		m.input.FocusedStyle.Text = m.st.warn
		m.input.BlurredStyle.Text = m.st.warn
	}
}

func (m *model) placeholderText() string {
	switch {
	case m.editing:
		return m.tr.t("tui.compose.editor_open", map[string]any{"Editor": editorName()})
	case m.surface == surfacePanes:
		return m.tr.t("tui.compose.placeholder_panes", nil)
	case m.isBusy():
		return m.placeholderBusyText()
	}
	return m.tr.t("tui.compose.placeholder_idle", nil)
}

func (m *model) isShellInput() bool {
	return strings.HasPrefix(m.input.Value(), shellPrefix)
}

func (m *model) isBusy() bool {
	tb := m.activeTab()
	return tb != nil && tb.busy
}

func (m *model) composerStyle() lipgloss.Style {
	switch {
	case m.editing:
		return m.st.violet
	case m.isShellInput():
		return m.st.warn
	default:
		return m.st.dim
	}
}

func (m *model) composerTitles() ([]string, string) {
	switch {
	case m.editing:
		return []string{
			m.st.violet.Bold(true).Render(m.tr.t("tui.compose.editor_title", nil)),
			m.st.dim.Render(footerSeparator + m.tr.t("tui.compose.draft_open_in", map[string]any{"Editor": editorName()})),
		}, m.st.dim.Render(m.editorTitle())
	case m.isShellInput():
		return append([]string{m.st.warn.Bold(true).Render("! SHELL")}, m.shellTitleDetail()...), m.st.dim.Render(m.editorTitle())
	case m.surface == surfacePanes:
		return []string{m.st.dim.Render(m.tr.t("tui.compose.to_prefix", nil)) + m.st.accent.Render(m.paneFocusName())}, m.st.dim.Render(m.panesTitleHint())
	}
	name := "herrscher"
	if tb := m.activeTab(); tb != nil && tb.label != "" {
		name = tb.label
	}
	return []string{m.st.text.Render(name)}, m.st.dim.Render(m.editorTitle())
}

func (m *model) shellTitleDetail() []string {
	var parts []string
	if info, ok := m.activeInfo(); ok && info.Dir != "" {
		parts = append(parts, m.st.dim.Render(footerSeparator+m.tr.t("tui.compose.in_dir", map[string]any{"Dir": sessionDirLabel(info.Dir)})))
	}
	return append(parts, m.st.dim.Render(footerSeparator+m.tr.t("tui.compose.output_enters", nil)))
}

func sessionDirLabel(dir string) string {
	if index := strings.LastIndex(dir, worktreesMarker); index >= 0 {
		return dir[index+1:]
	}
	return shortPath(dir)
}

func editorName() string {
	argv := editorArgv("")
	return filepath.Base(argv[0])
}

func (m *model) composerFoots() (string, string) {
	footRight := m.doubleEscHint()
	if m.isBusy() {
		footRight = m.labelledHint(actCancel, m.tr.t("tui.common.interrupt", nil), chatKeyStack()...)
	}
	switch {
	case m.isShellInput():
		return m.footShellText(), footRight
	case m.isBusy():
		return m.footBusyText(), footRight
	default:
		return m.footIdleText(), footRight
	}
}

func (m *model) placeholderBusyText() string {
	return m.tr.t("tui.compose.busy_placeholder", map[string]any{"Actions": joinWith(", ",
		m.labelledHint(actSubmit, m.tr.t("tui.compose.queue_submit", nil), chatKeyStack()...),
		m.labelledHint(actQueueRedirect, m.tr.t("tui.compose.redirect", nil), chatKeyStack()...),
	)})
}

func (m *model) footIdleText() string {
	return joinNonEmpty(
		m.labelledHint(actSubmit, m.tr.t("tui.common.send", nil), chatKeyStack()...),
		m.tr.t("tui.compose.hint_file", nil), m.tr.t("tui.compose.hint_command", nil), "! shell",
		m.labelledHint(actHistorySearch, m.tr.t("tui.common.history", nil), chatKeyStack()...),
		m.labelledHint(actStash, m.tr.t("tui.compose.stash", nil), chatKeyStack()...),
	)
}

func (m *model) footBusyText() string {
	return joinNonEmpty(
		m.labelledHint(actSubmit, m.tr.t("tui.compose.queued", nil), chatKeyStack()...),
		m.labelledHint(actQueueRedirect, m.tr.t("tui.compose.redirect_verb", nil), chatKeyStack()...),
		m.labelledHint(actQueueSubmit, m.tr.t("tui.compose.send_queue", nil), chatKeyStack()...),
		"! shell",
		m.labelledHint(actHistorySearch, m.tr.t("tui.common.history", nil), chatKeyStack()...),
		m.labelledHint(actStash, m.tr.t("tui.compose.stash", nil), chatKeyStack()...),
	)
}

func (m *model) footShellText() string {
	return joinNonEmpty(
		m.labelledHint(actSubmit, m.tr.t("tui.compose.run", nil), chatKeyStack()...),
		m.tr.t("tui.compose.shell_policy", nil),
		m.labelledHint(actCancel, m.tr.t("tui.common.cancel", nil), chatKeyStack()...),
	)
}

func (m *model) doubleEscHint() string {
	glyph := m.hintIn(actCancel, chatKeyStack()...)
	return joinWith(" ", glyph, glyph, m.tr.t("tui.compose.back", nil))
}

func (m *model) editorTitle() string {
	return m.labelledHint(actExternalEditor, m.tr.t("tui.common.editor", nil), chatKeyStack()...)
}

func (m *model) panesTitleHint() string {
	return joinNonEmpty(
		m.labelledHint(actPaneClose, m.tr.t("tui.compose.pane_close", nil), ctxPane),
		m.labelledHint(actPaneZoom, m.tr.t("tui.compose.pane_zoom", nil), ctxPane),
	)
}

func segmentsOf(text string, paint func(string) string) []string {
	parts := strings.Split(text, footerSeparator)
	segments := make([]string, 0, len(parts))
	for index, part := range parts {
		if index > 0 {
			part = footerSeparator + part
		}
		segments = append(segments, paint(part))
	}
	return segments
}

func foldText(text string, width int) string {
	return foldSegments(segmentsOf(text, plainText), width)
}

func boxEdge(corners [2]string, left []string, right string, width int, style lipgloss.Style, horizontal string) string {
	inner := width - composerBorderCols
	rightPart := ""
	if right != "" {
		rightPart = " " + right + " "
	}
	leftBudget := inner - edgeSpacing - lipgloss.Width(rightPart)
	if leftBudget < 1 {
		rightPart = ""
		leftBudget = inner - edgeSpacing
	}
	leftPart := " " + foldSegments(left, leftBudget) + " "
	fill := max(inner-lipgloss.Width(leftPart)-lipgloss.Width(rightPart), 0)
	return style.Render(corners[0]) + leftPart + style.Render(strings.Repeat(horizontal, fill)) + rightPart + style.Render(corners[1])
}

func (m *model) composerZone() []string {
	body := strings.Split(m.inputRow(), "\n")
	if !m.composerBoxed() {
		out := make([]string, len(body))
		for i, line := range body {
			out[i] = cutText(line, m.innerWidth())
		}
		return out
	}
	boxes := m.st.boxes(m.caps.Boxes)
	width := m.innerWidth()
	style := m.composerStyle()
	titleLeft, titleRight := m.composerTitles()
	footLeft, footRight := m.composerFoots()
	side := style.Render(boxes.v)
	zone := []string{boxEdge([2]string{boxes.tl, boxes.tr}, titleLeft, titleRight, width, style, boxes.h)}
	for _, line := range body {
		line = cutText(line, width-composerChrome)
		pad := width - composerChrome - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		zone = append(zone, side+" "+line+strings.Repeat(" ", pad)+" "+side)
	}
	zone = append(zone, boxEdge([2]string{boxes.bl, boxes.br}, segmentsOf(footLeft, m.paintDim), m.st.dim.Render(footRight), width, style, boxes.h))
	return zone
}

func plainText(text string) string {
	return text
}

func (m *model) paintDim(text string) string {
	return m.st.dim.Render(text)
}
