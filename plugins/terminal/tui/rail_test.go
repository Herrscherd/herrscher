package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRailShowsBrandAndTabs(t *testing.T) {
	m := sizedTestModel(t, 74)
	m.active = "c1"
	m.order = []string{"c1", "c2"}
	m.tabs = map[string]*tab{
		"c1": {label: "neublox"},
		"c2": {label: "herrscher", unread: true},
	}
	row := m.railRow()
	if lipgloss.Width(row) > 74 {
		t.Fatalf("rail is %d columns, want <= 74", lipgloss.Width(row))
	}
	for _, want := range []string{"HERRSCHER", "neublox", "herrscher", glyphUnread} {
		if !strings.Contains(row, want) {
			t.Fatalf("rail %q missing %q", row, want)
		}
	}
}

func TestRailKeepsActiveTabWhenTooNarrow(t *testing.T) {
	m := sizedTestModel(t, 12)
	m.active = "c1"
	m.order = []string{"c1", "c2", "c3"}
	m.tabs = map[string]*tab{
		"c1": {label: "neublox"},
		"c2": {label: "herrscher"},
		"c3": {label: "docs-site"},
	}
	row := m.railRow()
	if lipgloss.Width(row) > 12 {
		t.Fatalf("rail is %d columns, want <= 12", lipgloss.Width(row))
	}
	if !strings.Contains(row, "neublox") {
		t.Fatalf("rail %q dropped the active tab", row)
	}
}
