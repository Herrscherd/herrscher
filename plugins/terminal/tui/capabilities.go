package tui

import "strings"

// Every feature that draws something the terminal may not be able to draw — an
// image, a hyperlink, a truecolor swatch — needs the same answer to the same
// question. Asked once, here, and read from the model thereafter: two detections
// of the same capability drift, and the one that drifts is always the one the
// operator is looking at.
//
// Probing is by environment alone. A terminal can be interrogated over the wire
// (DA1, XTGETTCAP), but that means writing a query and waiting for a reply on a
// stdin bubbletea has not started reading yet, and a terminal that never answers
// costs the startup a timeout. The environment is what every terminal already
// agrees to say about itself.

// Graphics is the inline-image protocol a terminal speaks, in descending order
// of fidelity. The zero value is the honest default: no protocol at all.
type Graphics int

const (
	GraphicsNone Graphics = iota
	GraphicsSixel
	GraphicsKitty
)

func (g Graphics) String() string {
	switch g {
	case GraphicsKitty:
		return "kitty"
	case GraphicsSixel:
		return "sixel"
	}
	return "none"
}

// Colour is the depth the terminal renders, ascending. The zero value is 16
// colours: what a terminal is assumed to have when it says nothing.
type Colour int

const (
	Colour16 Colour = iota
	Colour256
	ColourTrue
)

func (c Colour) String() string {
	switch c {
	case ColourTrue:
		return "truecolor"
	case Colour256:
		return "256"
	}
	return "16"
}

// Capabilities is what this terminal can do, resolved once at startup. Terminal
// is the best human-readable name found, carried for the diagnostic screen: a
// capability the operator disagrees with is only arguable if they can see which
// terminal it was resolved for.
type Capabilities struct {
	Graphics   Graphics
	Hyperlinks bool
	Mouse      bool
	Colour     Colour
	Terminal   string
}

// kittyGraphicsPrograms names TERM_PROGRAM values whose terminals implement the
// kitty graphics protocol besides kitty itself.
var kittyGraphicsPrograms = map[string]bool{"ghostty": true, "WezTerm": true}

// hyperlinkPrograms names the TERM_PROGRAM values known to honour OSC 8. The
// list is short on purpose: a terminal that ignores OSC 8 prints nothing (the
// sequence is swallowed), but one that half-implements it prints the escape as
// text, and a transcript full of escape litter is worse than plain styled text.
var hyperlinkPrograms = map[string]bool{
	"ghostty": true, "WezTerm": true, "iTerm.app": true, "kitty": true,
}

// Probe resolves what the terminal described by env (an os.Getenv-like lookup)
// can do. It is pure in its input so the whole matrix is a table test, and it
// runs exactly once — nothing re-probes mid-session, because a capability that
// changed under a running frame would leave half the screen drawn for the old one.
func Probe(env func(string) string) Capabilities {
	term, program := env("TERM"), env("TERM_PROGRAM")
	caps := Capabilities{
		Graphics:   probeGraphics(env, term, program),
		Hyperlinks: hyperlinkPrograms[program] || strings.Contains(term, "kitty"),
		Colour:     probeColour(env("COLORTERM"), term),
		Terminal:   terminalName(term, program),
	}
	// A terminal that names itself is one bubbletea can put into mouse-reporting
	// mode; "dumb" and an unset TERM are the two cases that mean the other end is
	// not an interactive terminal at all.
	caps.Mouse = term != "" && term != "dumb"
	return caps
}

func probeGraphics(env func(string) string, term, program string) Graphics {
	if env("KITTY_WINDOW_ID") != "" || strings.Contains(term, "kitty") || kittyGraphicsPrograms[program] {
		return GraphicsKitty
	}
	if strings.Contains(term, "sixel") || program == "mlterm" {
		return GraphicsSixel
	}
	return GraphicsNone
}

func probeColour(colorterm, term string) Colour {
	switch strings.ToLower(colorterm) {
	case "truecolor", "24bit":
		return ColourTrue
	}
	if strings.Contains(term, "256color") {
		return Colour256
	}
	return Colour16
}

// terminalName is what to call this terminal on the diagnostic screen.
// TERM_PROGRAM is the name of the application and TERM the name of the
// termcap entry, so the former is preferred when both are present — except for
// kitty, which sets no TERM_PROGRAM and is better known by its own name than by
// "xterm-kitty".
func terminalName(term, program string) string {
	if strings.Contains(term, "kitty") {
		return "kitty"
	}
	if program != "" {
		return program
	}
	if term != "" {
		return term
	}
	return "unknown"
}
