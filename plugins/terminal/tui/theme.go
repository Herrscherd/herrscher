package tui

import "github.com/charmbracelet/lipgloss"

const (
	glyphTool      = "●"
	glyphResult    = "⎿"
	glyphPrompt    = ">"
	glyphCursor    = "❯"
	glyphUnread    = "•"
	glyphThinking  = "✳"
	glyphNotice    = "·"
	glyphError     = "✗"
	glyphFold      = "▶"
	glyphMoreLines = "↵"
	glyphBolt      = "⚡"
)

func familyGlyphs() map[string]string {
	return map[string]string{
		familyRead:   "◇",
		familyWrite:  "◈",
		familyRun:    "▸",
		familySearch: "⌕",
		familyWeb:    "⌖",
		familyAgent:  "◆",
	}
}

func familyGlyph(family string) string {
	if g, ok := familyGlyphs()[family]; ok {
		return g
	}
	return glyphTool
}

func spinFrames() []string {
	return []string{"✳", "✵", "✶", "✷", "✸", "✹"}
}

const (
	colText   = "#c6d0e0"
	colDim    = "#56637a"
	colAccent = "#6fd0e8"
	colViolet = "#9d8cf0"
	colGreen  = "#5fbf8f"
	colWarn   = "#e2b04a"
	colRed    = "#e04760"
	colSelBg  = "#16202e"
)

type uiStyles struct {
	text       lipgloss.Style
	dim        lipgloss.Style
	accent     lipgloss.Style
	user       lipgloss.Style
	green      lipgloss.Style
	red        lipgloss.Style
	warn       lipgloss.Style
	spinner    lipgloss.Style
	tool       lipgloss.Style
	thinking   lipgloss.Style
	scrollback lipgloss.Style
	link       lipgloss.Style
	chip       lipgloss.Style
	overlaySel lipgloss.Style
	sep        string
}

func newStyles() uiStyles {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(colDim))
	return uiStyles{
		text:       lipgloss.NewStyle().Foreground(lipgloss.Color(colText)),
		dim:        dim,
		accent:     lipgloss.NewStyle().Foreground(lipgloss.Color(colAccent)),
		user:       lipgloss.NewStyle().Foreground(lipgloss.Color(colDim)),
		green:      lipgloss.NewStyle().Foreground(lipgloss.Color(colGreen)),
		red:        lipgloss.NewStyle().Foreground(lipgloss.Color(colRed)),
		warn:       lipgloss.NewStyle().Foreground(lipgloss.Color(colWarn)),
		spinner:    lipgloss.NewStyle().Foreground(lipgloss.Color(colAccent)),
		tool:       lipgloss.NewStyle().Foreground(lipgloss.Color(colAccent)),
		thinking:   lipgloss.NewStyle().Foreground(lipgloss.Color(colViolet)).Italic(true),
		scrollback: lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(colDim)),
		link:       lipgloss.NewStyle().Foreground(lipgloss.Color(colAccent)).Underline(true),
		chip:       lipgloss.NewStyle().Foreground(lipgloss.Color(colText)).Background(lipgloss.Color(colSelBg)).Padding(0, 1),
		overlaySel: lipgloss.NewStyle().Background(lipgloss.Color(colSelBg)).Foreground(lipgloss.Color(colAccent)),
		sep:        dim.Render(" │ "),
	}
}

const workingVerb = "Herrschering…"
