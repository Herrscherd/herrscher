package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func sizedTestModel(t *testing.T, width int) *model {
	t.Helper()
	m := newModel(&fakeBackend{})
	m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
	return m
}

func TestBoxRunesFallBackToAscii(t *testing.T) {
	tl, tr, bl, br, h, v := boxRunes(false)
	for _, got := range []string{tl, tr, bl, br} {
		if got != "+" {
			t.Fatalf("corner = %q, want +", got)
		}
	}
	if h != "-" || v != "|" {
		t.Fatalf("edges = %q %q, want - |", h, v)
	}
}

func TestBoxRunesUseRoundedWhenSupported(t *testing.T) {
	tl, _, _, br, h, v := boxRunes(true)
	if tl != "╭" || br != "╯" || h != "─" || v != "│" {
		t.Fatalf("rounded runes wrong: %q %q %q %q", tl, br, h, v)
	}
}

func TestComposerZoneDrawsThreeLinesAtFullWidth(t *testing.T) {
	m := sizedTestModel(t, 74)
	m.caps.Boxes = true
	m.applySize()
	zone := m.composerZone()
	if len(zone) != 3 {
		t.Fatalf("zone has %d lines, want 3", len(zone))
	}
	for i, line := range zone {
		if w := lipgloss.Width(line); w != 74 {
			t.Fatalf("line %d is %d columns, want 74", i, w)
		}
	}
	if !strings.Contains(zone[0], "╭") || !strings.Contains(zone[2], "╯") {
		t.Fatalf("box corners missing: %q / %q", zone[0], zone[2])
	}
}

func TestComposerZoneDropsBoxWhenNarrow(t *testing.T) {
	m := sizedTestModel(t, composerBoxMinWidth-1)
	m.caps.Boxes = true
	m.applySize()
	zone := m.composerZone()
	if len(zone) != 1 {
		t.Fatalf("narrow zone has %d lines, want 1", len(zone))
	}
	if strings.Contains(zone[0], "╭") {
		t.Fatalf("narrow zone still boxed: %q", zone[0])
	}
}

func TestComposerTextWidthReservesTheBorder(t *testing.T) {
	m := sizedTestModel(t, 74)
	if got := m.composerTextWidth(); got != 70 {
		t.Fatalf("text width = %d, want 70", got)
	}
	narrow := sizedTestModel(t, composerBoxMinWidth-1)
	if got := narrow.composerTextWidth(); got != composerBoxMinWidth-1 {
		t.Fatalf("narrow text width = %d, want %d", got, composerBoxMinWidth-1)
	}
}

func TestComposerZoneBoxesInAsciiWhenTheTerminalCannotDrawRunes(t *testing.T) {
	m := sizedTestModel(t, 74)
	m.caps.Boxes = false
	m.applySize()
	zone := m.composerZone()
	if len(zone) != 3 {
		t.Fatalf("ascii zone has %d lines, want 3", len(zone))
	}
	if !strings.Contains(zone[0], "+") || !strings.Contains(zone[0], "-") {
		t.Fatalf("ascii box missing its top edge: %q", zone[0])
	}
	if strings.Contains(zone[0], "╭") {
		t.Fatalf("ascii box must not use rounded runes: %q", zone[0])
	}
}
