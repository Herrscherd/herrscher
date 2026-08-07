package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Degradation elsewhere in this package is deliberately silent: a feature reads
// the probe, picks its best available rendering and says nothing. That is right
// at the point of use and useless when something is missing — from the outside,
// "the image does not show up" looks exactly like a bug. This screen is the one
// place the probe is spoken aloud.

// diagRow is one capability as the operator reads it: what it is called, what it
// resolved to, and what that buys. The third column is the point — a value with
// no consequence attached is a number to look up somewhere else.
type diagRow struct {
	name    string
	value   string
	feature string
}

// diagRows renders caps as the rows of the screen. A capability that resolved to
// nothing keeps its row and names its fallback: hiding it would say the feature
// does not exist, when what is true is that this terminal renders it plainer.
func diagRows(caps Capabilities) []diagRow {
	graphics := diagRow{name: "graphics", value: caps.Graphics.String()}
	switch caps.Graphics {
	case GraphicsKitty:
		graphics.feature = "inline images, full resolution"
	case GraphicsSixel:
		graphics.feature = "inline images, sixel resolution"
	default:
		graphics.feature = "inline images as unicode half-blocks"
	}

	links := diagRow{name: "hyperlinks", value: yesNo(caps.Hyperlinks), feature: "links open from the terminal (OSC 8)"}
	if !caps.Hyperlinks {
		links.feature = "links are styled text, opened with the open key"
	}

	mouse := diagRow{name: "mouse", value: yesNo(caps.Mouse), feature: "wheel scrolling and click-to-focus"}
	if !caps.Mouse {
		mouse.feature = "scrolling by pgup/pgdn"
	}

	colour := diagRow{name: "colour", value: caps.Colour.String(), feature: "the palette exactly as designed"}
	if caps.Colour != ColourTrue {
		colour.feature = "the palette downsampled to the nearest colour"
	}

	return []diagRow{graphics, links, mouse, colour}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// diagValueWidth pads the value column so the features line up. A ragged pair of
// columns reads as prose; an aligned one reads as a record, which is what it is.
const diagValueWidth = 12

// diagnosticsView renders the capability screen for caps: the terminal it was
// resolved for, one row per capability, and a closing line saying which
// terminals get everything — so an operator on a plain one knows the plainer
// rendering is the answer and not a failure.
func diagnosticsView(caps Capabilities) string {
	var b strings.Builder
	b.WriteString(dimStyle.Render("terminal") + "  " + textStyle.Render(caps.Terminal))
	for _, r := range diagRows(caps) {
		pad := strings.Repeat(" ", max(1, diagValueWidth-len(r.value)))
		b.WriteString("\n" + dimStyle.Render("  "+r.name+strings.Repeat(" ", max(1, factWidth-len(r.name)))) +
			textStyle.Render(r.value) + pad + dimStyle.Render(r.feature))
	}
	b.WriteString("\n" + dimStyle.Render("  kitty, ghostty and WezTerm get every rendering; anywhere else the"))
	b.WriteString("\n" + dimStyle.Render("  picture is drawn in half-blocks and a link stays selectable text."))
	return b.String()
}

// openDiagnostics opens the read-only capability screen.
func (m *model) openDiagnostics() { m.diagOpen = true }

// diagView renders the open screen with its own header, or "" when closed.
func (m *model) diagView() string {
	if !m.diagOpen {
		return ""
	}
	return dimStyle.Render("capabilities — Esc close") + "\n" + diagnosticsView(m.caps) +
		"\n" + m.remoteImageRow()
}

// remoteImageRow says whether an image the agent merely links to will be drawn.
// That does not depend on the terminal but on the gateway's attachment
// allowlist, and today the terminal gateway declares no hosts — so the answer is
// no, always, and the code that fetches never runs. It belongs on this screen
// rather than in a comment nobody reading the transcript will find: an operator
// watching a URL stay a URL is owed the reason, and "off" is a reason where
// silence is a bug report.
func (m *model) remoteImageRow() string {
	value, feature := "off", "an image the agent links to stays a link"
	if m.imageFetcher != nil && len(m.imageHosts) > 0 {
		value = "on"
		feature = "images fetched from " + strings.Join(m.imageHosts, ", ")
	}
	name := "remote images"
	return dimStyle.Render("  "+name+strings.Repeat(" ", max(1, factWidth-len(name)))) +
		textStyle.Render(value) + strings.Repeat(" ", max(1, diagValueWidth-len(value))) +
		dimStyle.Render(feature)
}

// diagHeight is the rendered row count of the open screen (0 when closed), so
// chromeHeight can reserve space for it.
func (m *model) diagHeight() int {
	if !m.diagOpen {
		return 0
	}
	return lipgloss.Height(m.diagView())
}
