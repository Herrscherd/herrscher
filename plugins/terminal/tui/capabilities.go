package tui

import "strings"

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
	Boxes      bool
	Terminal   string
}

var kittyGraphicsPrograms = map[string]bool{"ghostty": true, "WezTerm": true}

var hyperlinkPrograms = map[string]bool{
	"ghostty": true, "WezTerm": true, "iTerm.app": true, "kitty": true,
}

// Probe resolves what the terminal described by env (an os.Getenv-like lookup)
// can do. It is pure in its input so the whole matrix is a table test, and it
// runs exactly once. Nothing re-probes mid-session, because a capability that
// changed under a running frame would leave half the screen drawn for the old one.
func Probe(env func(string) string) Capabilities {
	term, program := env("TERM"), env("TERM_PROGRAM")
	caps := Capabilities{
		Graphics:   probeGraphics(env, term, program),
		Hyperlinks: hyperlinkPrograms[program] || strings.Contains(term, "kitty"),
		Colour:     probeColour(env("COLORTERM"), term),
		Terminal:   terminalName(term, program),
	}
	caps.Boxes = probeBoxes(env)
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

func probeBoxes(env func(string) string) bool {
	if env("HERRSCHER_TUI_PLAIN") != "" {
		return false
	}
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := env(key); v != "" {
			upper := strings.ToUpper(v)
			return strings.Contains(upper, "UTF-8") || strings.Contains(upper, "UTF8")
		}
	}
	return false
}
