package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// After rendering the strip, clicking the midpoint of each recorded tab span
// must map back to that tab's session, and a click off the strip row must miss.
func TestTabAtRoutesClickToTab(t *testing.T) {
	m := newTestModel()
	m.width = 80 // wide enough that no tab is elided
	m.ensureTab("alpha")
	m.ensureTab("bravo")
	m.ensureTab("charlie")
	m.active = "alpha"

	m.tabStrip() // populates m.tabHits

	if len(m.tabHits) != 3 {
		t.Fatalf("want a hitbox per visible tab, got %d", len(m.tabHits))
	}
	for _, h := range m.tabHits {
		mid := (h.x0 + h.x1) / 2
		if got := m.tabAt(mid, tabStripScreenY); got != h.ch {
			t.Fatalf("click at x=%d (tab %q) routed to %q", mid, h.ch, got)
		}
	}
	// Off the strip row: no tab.
	if got := m.tabAt(m.tabHits[0].x0, tabStripScreenY+1); got != "" {
		t.Fatalf("click off strip row routed to %q", got)
	}
	// Left of the first tab (in the border/padding gutter): no tab.
	if got := m.tabAt(0, tabStripScreenY); got != "" {
		t.Fatalf("click in gutter routed to %q", got)
	}
}

// A left-click on an inactive tab, fed through Update as a real MouseMsg, must
// focus that tab and clear its unread marker.
func TestMouseClickActivatesTab(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.ready = true
	m.ensureTab("alpha")
	m.ensureTab("bravo")
	m.active = "alpha"
	m.tabs["bravo"].unread = true

	m.tabStrip() // record hitboxes as a render would
	var bravo tabHit
	for _, h := range m.tabHits {
		if h.ch == "bravo" {
			bravo = h
		}
	}
	click := tea.MouseMsg{
		X:      (bravo.x0 + bravo.x1) / 2,
		Y:      tabStripScreenY,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}
	m.Update(click)

	if m.active != "bravo" {
		t.Fatalf("click did not focus bravo: active=%q", m.active)
	}
	if m.tabs["bravo"].unread {
		t.Fatal("focusing bravo must clear its unread marker")
	}
}
