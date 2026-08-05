package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestBannerCarriesTheActiveTab is the point of the banner: which session you are
// typing into must be readable without opening a picker.
func TestBannerCarriesTheActiveTab(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.ensureTab("alpha")
	m.ensureTab("beta")
	m.active = "beta"
	out := m.bannerRow()
	if !strings.Contains(out, "HERRSCHER") {
		t.Fatalf("banner must carry the brand: %q", out)
	}
	if !strings.Contains(out, glyphCursor+" "+m.tabs["beta"].label) {
		t.Fatalf("the active tab must be marked: %q", out)
	}
	if !strings.Contains(out, m.tabs["alpha"].label) {
		t.Fatalf("an inactive tab must still be listed: %q", out)
	}
	if w := lipgloss.Width(out); w > 80 {
		t.Fatalf("banner width %d exceeds 80: %q", w, out)
	}
}

// TestBannerDropsTheBrandBeforeTheTabs checks the narrow case resolves the right
// way: which session you are in outranks the name of the running program.
func TestBannerDropsTheBrandBeforeTheTabs(t *testing.T) {
	m := newTestModel()
	m.width = 20
	m.ensureTab("alpha")
	out := m.bannerRow()
	if strings.Contains(out, "HERRSCHER") {
		t.Fatalf("a narrow banner must drop the brand: %q", out)
	}
	if w := lipgloss.Width(out); w > 20 {
		t.Fatalf("narrow banner width %d exceeds 20: %q", w, out)
	}
}

// TestUnreadTabCarriesItsPip keeps a background session that produced output from
// looking like one that did not.
func TestUnreadTabCarriesItsPip(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.ensureTab("alpha")
	m.ensureTab("beta")
	m.tabs["beta"].unread = true
	if !strings.Contains(m.tabStrip(), glyphUnread) {
		t.Fatalf("an unread tab must carry its pip: %q", m.tabStrip())
	}
}

// TestEmptyTabShowsAnEmptyState is the regression for the black screen: a fresh
// tab used to render nothing at all.
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

// TestChromeHeightCountsTheBannerAndRule guards the arithmetic that keeps the
// viewport from overflowing: View renders exactly chromeHeight non-viewport rows.
func TestChromeHeightCountsTheBannerAndRule(t *testing.T) {
	m := newTestModel()
	m.ready = true
	m.width, m.height = 80, 24
	m.applySize()
	rows := strings.Count(m.View(), "\n") + 1
	want := m.vp.Height + m.chromeHeight()
	// bubbletea draws one row it does not count; the invariant is that View never
	// renders more rows than the window has.
	if rows > want {
		t.Fatalf("View renders %d rows, budget is %d", rows, want)
	}
}

// TestStatusRowIsClippedToOneLine is the layout invariant chromeHeight depends
// on: the status bar grows with the session, and a row that wrapped would push
// every line under it down by one with nothing accounting for it.
func TestStatusRowIsClippedToOneLine(t *testing.T) {
	m := newTestModel()
	m.width = 30
	row := m.statusRow(dimStyle.Render(strings.Repeat("very-long-session-name ", 10)))
	if strings.Contains(row, "\n") {
		t.Fatalf("the status row must stay one line: %q", row)
	}
	if w := lipgloss.Width(row); w > 30 {
		t.Fatalf("status row width %d exceeds 30", w)
	}
}

// TestNarrowBannerKeepsTheActiveTab guards the reduction order: clipping the
// strip cuts from the right, which would drop the very tab you are typing into.
func TestNarrowBannerKeepsTheActiveTab(t *testing.T) {
	m := newTestModel()
	m.width = 18
	for _, ch := range []string{"alpha-session", "beta-session", "gamma-session"} {
		m.ensureTab(ch)
	}
	m.active = "gamma-session"
	out := m.bannerRow()
	if !strings.Contains(out, "gamma") {
		t.Fatalf("a banner too narrow for the strip must keep the active tab: %q", out)
	}
	if w := lipgloss.Width(out); w > 18 {
		t.Fatalf("banner width %d exceeds 18: %q", w, out)
	}
}
