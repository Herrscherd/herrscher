package tui

import "github.com/charmbracelet/lipgloss"

// Glyphs give the TUI its visual language; kept together so the language is one edit.
const (
	glyphBrand    = "⟡"
	glyphAgent    = "◆"
	glyphYou      = "›"
	glyphThinking = "◇"
	glyphBusy     = "⟳"
	glyphUnread   = "•"
)

// spinFrames animate the "thinking" indicator; advanced on the fast working-tick.
var spinFrames = []string{"◇", "◈", "◆", "◈"}

// Styles are role-named so every render site references a role, not a raw colour.
// The terminal is a committed dark surface, so colours are fixed hex (lipgloss
// downsamples on limited terminals; role is also carried by glyph and position).
var (
	humanStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#b18cff")) // you
	replyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#56e1cd"))            // agent
	costStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#d0aa63"))            // cost
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#55647f"))            // dim

	brandStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#56e1cd"))
	workingStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffcf6b"))
	unreadStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#b18cff"))
	activeTabStyle   = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("#ffffff"))
	inactiveTabStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#55647f"))

	paletteBorder   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#56e1cd")).Padding(0, 1)
	paletteSelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#56e1cd"))
)
