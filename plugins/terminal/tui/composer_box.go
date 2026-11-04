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
	composerPrompt   = "> "
	placeholderIdle  = "Tape un message, / ou @, ou !commande"
	placeholderPanes = "Le message part dans le volet en surbrillance"
	ellipsis         = "…"
	worktreesMarker  = "/.worktrees/"
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
		return editorName() + " est ouvert sur le brouillon, :wq pour revenir"
	case m.surface == surfacePanes:
		return placeholderPanes
	case m.isBusy():
		return m.placeholderBusyText()
	}
	return placeholderIdle
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
			m.st.violet.Bold(true).Render("Éditeur"),
			m.st.dim.Render(footerSeparator + "brouillon ouvert dans " + editorName()),
		}, m.st.dim.Render(m.editorTitle())
	case m.isShellInput():
		return append([]string{m.st.warn.Bold(true).Render("! SHELL")}, m.shellTitleDetail()...), m.st.dim.Render(m.editorTitle())
	case m.surface == surfacePanes:
		return []string{m.st.dim.Render("vers ") + m.st.accent.Render(m.paneFocusName())}, m.st.dim.Render(m.panesTitleHint())
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
		parts = append(parts, m.st.dim.Render(footerSeparator+"dans "+sessionDirLabel(info.Dir)))
	}
	return append(parts, m.st.dim.Render(footerSeparator+"la sortie entre dans la session"))
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
		footRight = m.labelledHint(actCancel, "interrompre", chatKeyStack()...)
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
	return "Écris pendant que l’agent travaille : " + joinWith(", ",
		m.labelledHint(actSubmit, "met en file", chatKeyStack()...),
		m.labelledHint(actQueueRedirect, "réoriente", chatKeyStack()...),
	)
}

func (m *model) footIdleText() string {
	return joinNonEmpty(
		m.labelledHint(actSubmit, "envoyer", chatKeyStack()...),
		"@ fichier", "/ commande", "! shell",
		m.labelledHint(actHistorySearch, "historique", chatKeyStack()...),
		m.labelledHint(actStash, "de côté", chatKeyStack()...),
	)
}

func (m *model) footBusyText() string {
	return joinNonEmpty(
		m.labelledHint(actSubmit, "en file", chatKeyStack()...),
		m.labelledHint(actQueueRedirect, "réorienter", chatKeyStack()...),
		m.labelledHint(actQueueSubmit, "envoyer la file", chatKeyStack()...),
		"! shell",
		m.labelledHint(actHistorySearch, "historique", chatKeyStack()...),
		m.labelledHint(actStash, "de côté", chatKeyStack()...),
	)
}

func (m *model) footShellText() string {
	return joinNonEmpty(
		m.labelledHint(actSubmit, "lancer", chatKeyStack()...),
		"la commande passe par la politique d’outils de l’hôte",
		m.labelledHint(actCancel, "annuler", chatKeyStack()...),
	)
}

func (m *model) doubleEscHint() string {
	glyph := m.hintIn(actCancel, chatKeyStack()...)
	return joinWith(" ", glyph, glyph, "retour")
}

func (m *model) editorTitle() string {
	return m.labelledHint(actExternalEditor, "éditeur", chatKeyStack()...)
}

func (m *model) panesTitleHint() string {
	return joinNonEmpty(
		m.labelledHint(actPaneClose, "quitter", ctxPane),
		m.labelledHint(actPaneZoom, "agrandir", ctxPane),
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
