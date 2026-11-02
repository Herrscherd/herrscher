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

var (
	textStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colText))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(colDim))
	accentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(colAccent))
	userStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colDim))
	greenStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colGreen))
	redStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(colRed))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colWarn))
	spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colAccent))

	toolStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(colAccent))
	thinkingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colViolet)).Italic(true)

	scrollbackStyle = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(colDim))

	linkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colAccent)).Underline(true)

	chipStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colText)).Background(lipgloss.Color(colSelBg)).Padding(0, 1)
)

const workingVerb = "Herrschering…"
