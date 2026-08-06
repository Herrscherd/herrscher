package tui

import "github.com/charmbracelet/lipgloss"

// Glyphs give the TUI its Claude-parity visual language; kept together so the
// language is one edit. There is no message-block spine and no brand/tab glyph:
// the flow is full-width prose, marked only by a dim prompt and tool bullets.
const (
	glyphTool     = "●" // a tool-call line of an unrecognised tool
	glyphResult   = "⎿" // a tool-result / continuation line
	glyphPrompt   = ">" // the user echo and composer prompt
	glyphCursor   = "❯" // the selected row in an inline menu
	glyphUnread   = "•" // an unread background session, shown only in the switch picker
	glyphThinking = "✳" // a reasoning summary
	glyphNotice   = "·" // a turn reset / abandoned marker
	glyphError    = "✗" // a failed command or an error surfaced by the host
	glyphBolt     = "⚡" // the terminal's own mark: it leads the status bar and the agent's rule
)

// familyGlyphs give each tool family its own mark, so scanning the gutter reads
// as what the agent did rather than as one undifferentiated column.
var familyGlyphs = map[string]string{
	familyRead:   "◇",
	familyWrite:  "◈",
	familyRun:    "▸",
	familySearch: "⌕",
	familyWeb:    "⌖",
	familyAgent:  "◆",
}

// familyGlyph is the mark for a tool family, falling back to the generic tool
// bullet for a line whose tool could not be recognised.
func familyGlyph(family string) string {
	if g, ok := familyGlyphs[family]; ok {
		return g
	}
	return glyphTool
}

// spinFrames animate the working indicator, a Claude-style rotating asterisk set
// advanced on the fast working-tick.
var spinFrames = []string{"✳", "✵", "✶", "✷", "✸", "✹"}

// The Herrscher palette: a cold instrument surface — cyan for what the machine
// is doing, violet for what it is thinking, and the Herrscher red kept for the
// one thing that must stop the eye. Colours are fixed hex (lipgloss downsamples
// on limited terminals); role is also carried by glyph and position, so a
// downsampled terminal stays legible.
const (
	colText   = "#c6d0e0"
	colDim    = "#56637a"
	colAccent = "#6fd0e8" // cyan: the machine's own voice — tools, chrome, spinner
	colViolet = "#9d8cf0" // violet: reasoning and the structure of prose
	colGreen  = "#5fbf8f"
	colWarn   = "#e2b04a" // the middle context palier: worth planning around, not yet urgent
	colRed    = "#e04760" // the Herrscher red: errors, removals, the alarm palier
	colSelBg  = "#16202e"
)

// Styles are role-named so every render site references a role, not a raw colour.
var (
	textStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colText))   // agent prose
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(colDim))    // status, hints, descriptions
	accentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(colAccent)) // the cyan accent: selection cursors, prompts
	userStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colDim))    // the dim user echo
	greenStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colGreen))  // success / additions
	redStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(colRed))    // errors / removals
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colWarn))   // a measurement approaching its ceiling
	spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colAccent)) // the working spinner + hint

	toolStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(colAccent))              // a tool call line
	thinkingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colViolet)).Italic(true) // a reasoning summary

	// scrollbackStyle marks replayed transcript lines seeded into a reopened tab,
	// dimmed and faint so past context reads as distinct from live output.
	scrollbackStyle = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color(colDim))

	// chipStyle renders an attachment as a pill token above the composer and under
	// the message that carried it.
	chipStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colText)).Background(lipgloss.Color(colSelBg)).Padding(0, 1)
)
