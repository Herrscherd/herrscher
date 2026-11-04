package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	composerChrome     = 4
	composerBorderCols = 2
)

const composerBoxMinWidth = 60

type boxGlyphs struct {
	tl, tr, bl, br, h, v string
}

func boxRunes(supported bool) boxGlyphs {
	if !supported {
		return boxGlyphs{"+", "+", "+", "+", "-", "|"}
	}
	return boxGlyphs{"╭", "╮", "╰", "╯", "─", "│"}
}

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
	placeholderIdle = "Écris un message…"
	footIdle        = "⏎ envoyer · @ fichier · / commande · ! shell · ⌃R historique · ⌃S de côté"
	footBusy        = "⏎ en file · ⌥⏎ réorienter · ⌃⏎ envoyer la file · ! shell · ⌃R historique · ⌃S de côté"
	footShell       = "⏎ lancer · la commande passe par la politique d'outils de l'hôte · échap annuler"
	footRightIdle   = "échap échap retour"
	footRightBusy   = "échap interrompre"
	footRightShell  = "échap annuler"
	titleEditor     = "⌃X⌃E éditeur"
	titleShell      = "! SHELL · la sortie entre dans la session"
	titleEditing    = "brouillon ouvert dans l'éditeur"
)

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
		return m.st.accent
	case m.isShellInput():
		return m.st.warn
	default:
		return m.st.dim
	}
}

func (m *model) composerTitles() (string, string) {
	switch {
	case m.editing:
		return m.st.accent.Render("Éditeur") + m.st.dim.Render(" · "+titleEditing), m.st.dim.Render(titleEditor)
	case m.isShellInput():
		return m.st.warn.Render("! SHELL") + m.st.dim.Render(strings.TrimPrefix(titleShell, "! SHELL")), m.st.dim.Render(titleEditor)
	}
	name := "herrscher"
	if tb := m.activeTab(); tb != nil && tb.label != "" {
		name = tb.label
	}
	return m.st.dim.Render(name), m.st.dim.Render(titleEditor)
}

func (m *model) composerFoots() (string, string) {
	switch {
	case m.isShellInput():
		return footShell, footRightShell
	case m.isBusy():
		return footBusy, footRightBusy
	default:
		return footIdle, footRightIdle
	}
}

func boxEdge(corners [2]string, left, right string, width int, style lipgloss.Style, horizontal string) string {
	inner := width - composerBorderCols
	leftPart := " " + left + " "
	rightPart := " " + right + " "
	if right == "" {
		rightPart = ""
	}
	fill := inner - lipgloss.Width(leftPart) - lipgloss.Width(rightPart)
	if fill < 1 {
		rightPart = ""
		leftPart = truncate(leftPart, inner-1)
		fill = inner - lipgloss.Width(leftPart)
	}
	if fill < 0 {
		fill = 0
	}
	return style.Render(corners[0]) + leftPart + style.Render(strings.Repeat(horizontal, fill)) + rightPart + style.Render(corners[1])
}

func (m *model) composerZone() []string {
	body := strings.Split(m.inputRow(), "\n")
	if !m.composerBoxed() {
		out := make([]string, len(body))
		for i, line := range body {
			out[i] = truncate(line, m.innerWidth())
		}
		return out
	}
	g := boxRunes(m.caps.Boxes)
	width := m.innerWidth()
	style := m.composerStyle()
	titleLeft, titleRight := m.composerTitles()
	footLeft, footRight := m.composerFoots()
	side := style.Render(g.v)
	zone := []string{boxEdge([2]string{g.tl, g.tr}, titleLeft, titleRight, width, style, g.h)}
	for _, line := range body {
		line = truncate(line, width-composerChrome)
		pad := width - composerChrome - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		zone = append(zone, side+" "+line+strings.Repeat(" ", pad)+" "+side)
	}
	zone = append(zone, boxEdge([2]string{g.bl, g.br}, m.st.dim.Render(footLeft), m.st.dim.Render(footRight), width, style, g.h))
	return zone
}
