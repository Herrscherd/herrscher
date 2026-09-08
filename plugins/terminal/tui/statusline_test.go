package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestStatusZoneIsOneLineWithHintOnTheRight(t *testing.T) {
	m := sizedTestModel(t, 74)
	m.active = "c1"
	m.tabs = map[string]*tab{"c1": {label: "neublox"}}
	zone := m.statusZone()
	if len(zone) != 1 {
		t.Fatalf("status zone has %d lines, want 1", len(zone))
	}
	if lipgloss.Width(zone[0]) > 74 {
		t.Fatalf("status is %d columns, want <= 74", lipgloss.Width(zone[0]))
	}
	if !strings.Contains(zone[0], "raccourcis") {
		t.Fatalf("status %q missing the help hint", zone[0])
	}
}

func TestStatusZoneDropsHintWhenNarrow(t *testing.T) {
	m := sizedTestModel(t, narrowStatus-1)
	m.active = "c1"
	m.tabs = map[string]*tab{"c1": {label: "neublox"}}
	if strings.Contains(m.statusZone()[0], "shortcuts") {
		t.Fatalf("narrow status %q kept the hint", m.statusZone()[0])
	}
}

func TestStatusZoneShowsFlashInsteadOfStatus(t *testing.T) {
	m := sizedTestModel(t, 74)
	m.active = "c1"
	m.tabs = map[string]*tab{"c1": {label: "neublox"}}
	m.flash = "copié"
	if !strings.Contains(m.statusZone()[0], "copié") {
		t.Fatalf("flash not shown: %q", m.statusZone()[0])
	}
}
