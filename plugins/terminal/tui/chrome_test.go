package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestEmptyTabShowsAnEmptyState(t *testing.T) {
	m := newTestModel()
	m.ready = true
	m.width, m.height = 80, 24
	m.vp.Width = 80
	m.ensureTab("alpha")
	out := m.thinkingContent()
	if strings.TrimSpace(out) == "" {
		t.Fatal("an empty tab must not render an empty screen")
	}
	if !strings.Contains(out, "commands") {
		t.Fatalf("the empty state must name a gesture: %q", out)
	}
}

func TestChromeHeightCountsTheBannerAndRule(t *testing.T) {
	m := newTestModel()
	m.ready = true
	m.width, m.height = 80, 24
	m.applySize()
	rows := strings.Count(m.View(), "\n") + 1
	want := m.vp.Height + m.chromeHeight()
	if rows != want {
		t.Fatalf("View renders %d rows, budget is %d", rows, want)
	}
}

func TestStatusZoneIsClippedToOneLine(t *testing.T) {
	m := newTestModel()
	m.width = 30
	m.active = "c1"
	m.tabs = map[string]*tab{"c1": {label: strings.Repeat("very-long-session-name ", 10)}}
	zone := m.statusZone()
	if len(zone) != 1 {
		t.Fatalf("the status zone must stay one line: %q", zone)
	}
	if w := lipgloss.Width(zone[0]); w > 30 {
		t.Fatalf("status row width %d exceeds 30", w)
	}
}

func TestEmptyStateListsFactsThenSessionsThenShortcuts(t *testing.T) {
	m := sizedTestModel(t, 74)
	m.active = "c1"
	m.order = []string{"c1", "c2"}
	m.tabs = map[string]*tab{"c1": {label: "docs-site"}, "c2": {label: "neublox"}}
	out := m.emptyState(74)
	iSessions := strings.Index(out, "other sessions")
	iShort := strings.Index(out, "commands")
	if iSessions < 0 || iShort < 0 || iSessions > iShort {
		t.Fatalf("empty state order wrong:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > 74 {
			t.Fatalf("line too wide: %q", line)
		}
	}
}
