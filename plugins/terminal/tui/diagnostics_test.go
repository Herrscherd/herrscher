package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The screen exists so "the image does not show up" can be told apart from a
// bug from the outside: every capability names what it resolved to and what
// that buys, so an operator can read the answer instead of guessing it.
func TestDiagnosticsNamesEachResolvedValueAndItsFeature(t *testing.T) {
	caps := Capabilities{
		Graphics:   GraphicsKitty,
		Hyperlinks: true,
		Mouse:      true,
		Colour:     ColourTrue,
		Terminal:   "kitty",
	}
	out := diagnosticsView(caps)
	for _, want := range []string{
		"kitty",      // the terminal it was resolved for, and the graphics value
		"truecolor",  // the colour value
		"graphics",   // the capability names
		"hyperlinks", //
		"mouse",      //
		"colour",     //
		"inline images",
		"OSC 8",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("diagnostics missing %q:\n%s", want, out)
		}
	}
}

// A capability that resolved to nothing is the one the operator most needs to
// read, so the row stays and names what it falls back to. Hiding it would say
// the feature does not exist rather than that this terminal renders it plainer.
func TestDiagnosticsNamesTheFallbackRatherThanHidingTheRow(t *testing.T) {
	out := diagnosticsView(Capabilities{Terminal: "xterm"})
	for _, want := range []string{"graphics", "none", "half-block", "hyperlinks", "styled text"} {
		if !strings.Contains(out, want) {
			t.Errorf("a plain terminal must still be told what it gets, missing %q:\n%s", want, out)
		}
	}
}

// The screen is reachable: /capabilities opens it, and Esc closes it.
func TestCapabilitiesCommandOpensAndClosesTheScreen(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("/capabilities")
	m.handleEnter()
	if !m.diagOpen {
		t.Fatal("/capabilities must open the diagnostic screen")
	}
	if !strings.Contains(m.diagView(), "graphics") {
		t.Fatalf("the open screen must render its rows: %q", m.diagView())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.diagOpen {
		t.Fatal("Esc must close the diagnostic screen")
	}
}
