package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDiagnosticsNamesEachResolvedValueAndItsFeature(t *testing.T) {
	caps := Capabilities{
		Graphics:   GraphicsKitty,
		Hyperlinks: true,
		Mouse:      true,
		Colour:     ColourTrue,
		Terminal:   "kitty",
	}
	out := diagnosticsView(caps, testStyles())
	for _, want := range []string{
		"kitty",
		"truecolor",
		"graphismes",
		"hyperliens",
		"souris",
		"couleur",
		"images en ligne",
		"OSC 8",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("diagnostics missing %q:\n%s", want, out)
		}
	}
}

func TestDiagnosticsNamesTheFallbackRatherThanHidingTheRow(t *testing.T) {
	out := diagnosticsView(Capabilities{Terminal: "xterm"}, testStyles())
	for _, want := range []string{"graphismes", "none", "demi-blocs", "hyperliens", "texte stylé"} {
		if !strings.Contains(out, want) {
			t.Errorf("a plain terminal must still be told what it gets, missing %q:\n%s", want, out)
		}
	}
}

func TestCapabilitiesCommandOpensAndClosesTheScreen(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("/capabilities")
	m.handleEnter()
	if !m.diagOpen {
		t.Fatal("/capabilities must open the diagnostic screen")
	}
	if !strings.Contains(m.diagView(), "graphismes") {
		t.Fatalf("the open screen must render its rows: %q", m.diagView())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.diagOpen {
		t.Fatal("Esc must close the diagnostic screen")
	}
}

func TestCapabilityScreenSaysRemoteImagesAreOff(t *testing.T) {
	m := newTestModel()
	m.openDiagnostics()
	off := m.diagView()
	if !strings.Contains(off, "remote images") || !strings.Contains(off, "stays a link") {
		t.Fatalf("the screen must say remote images are off and what that means:\n%s", off)
	}

	m.imageFetcher = func(context.Context, string) ([]byte, error) { return nil, nil }
	m.imageHosts = []string{"cdn.example.com"}
	if on := m.diagView(); !strings.Contains(on, "cdn.example.com") {
		t.Fatalf("a wired fetcher must name the hosts it may reach:\n%s", on)
	}
}
