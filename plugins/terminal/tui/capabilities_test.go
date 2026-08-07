package tui

import "testing"

// The probe is the one place the TUI decides what the terminal can do, so it is
// tested the way it is used: one row per terminal an operator actually runs,
// with every capability asserted at once. A row that only checked graphics would
// let a regression in colour or hyperlinks through unseen.
func TestProbe(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		graphics Graphics
		links    bool
		mouse    bool
		colour   Colour
		terminal string
	}{
		{
			name:     "kitty",
			env:      map[string]string{"TERM": "xterm-kitty", "KITTY_WINDOW_ID": "1", "COLORTERM": "truecolor"},
			graphics: GraphicsKitty, links: true, mouse: true, colour: ColourTrue, terminal: "kitty",
		},
		{
			name:     "ghostty",
			env:      map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "ghostty", "COLORTERM": "truecolor"},
			graphics: GraphicsKitty, links: true, mouse: true, colour: ColourTrue, terminal: "ghostty",
		},
		{
			name:     "wezterm",
			env:      map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "WezTerm", "COLORTERM": "24bit"},
			graphics: GraphicsKitty, links: true, mouse: true, colour: ColourTrue, terminal: "WezTerm",
		},
		{
			name:     "iTerm2",
			env:      map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "iTerm.app"},
			graphics: GraphicsNone, links: true, mouse: true, colour: Colour256, terminal: "iTerm.app",
		},
		{
			name:     "Terminal.app",
			env:      map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "Apple_Terminal"},
			graphics: GraphicsNone, links: false, mouse: true, colour: Colour256, terminal: "Apple_Terminal",
		},
		{
			name:     "mlterm",
			env:      map[string]string{"TERM": "mlterm", "TERM_PROGRAM": "mlterm"},
			graphics: GraphicsSixel, links: false, mouse: true, colour: Colour16, terminal: "mlterm",
		},
		{
			name:     "xterm-256color",
			env:      map[string]string{"TERM": "xterm-256color"},
			graphics: GraphicsNone, links: false, mouse: true, colour: Colour256, terminal: "xterm-256color",
		},
		{
			name:     "dumb",
			env:      map[string]string{"TERM": "dumb"},
			graphics: GraphicsNone, links: false, mouse: false, colour: Colour16, terminal: "dumb",
		},
		{
			name:     "empty environment",
			env:      map[string]string{},
			graphics: GraphicsNone, links: false, mouse: false, colour: Colour16, terminal: "unknown",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Probe(func(k string) string { return c.env[k] })
			if got.Graphics != c.graphics {
				t.Errorf("graphics = %v, want %v", got.Graphics, c.graphics)
			}
			if got.Hyperlinks != c.links {
				t.Errorf("hyperlinks = %v, want %v", got.Hyperlinks, c.links)
			}
			if got.Mouse != c.mouse {
				t.Errorf("mouse = %v, want %v", got.Mouse, c.mouse)
			}
			if got.Colour != c.colour {
				t.Errorf("colour = %v, want %v", got.Colour, c.colour)
			}
			if got.Terminal != c.terminal {
				t.Errorf("terminal = %q, want %q", got.Terminal, c.terminal)
			}
		})
	}
}

// The capability names reach the operator through the diagnostic screen, so they
// have to be words rather than the integers the enums are underneath.
func TestCapabilityNamesAreReadable(t *testing.T) {
	if got := GraphicsKitty.String(); got != "kitty" {
		t.Errorf("GraphicsKitty.String() = %q", got)
	}
	if got := GraphicsNone.String(); got != "none" {
		t.Errorf("GraphicsNone.String() = %q", got)
	}
	if got := ColourTrue.String(); got != "truecolor" {
		t.Errorf("ColourTrue.String() = %q", got)
	}
	if got := Colour16.String(); got != "16" {
		t.Errorf("Colour16.String() = %q", got)
	}
}
