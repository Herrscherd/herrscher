package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func railChannelNames() []string {
	return []string{"alfa", "bravo", "cite", "delta", "echo", "fox", "golf", "hotel", "india", "juliet"}
}

func railModel(t *testing.T, width int, names ...string) *model {
	t.Helper()
	m := sizedTestModel(t, width)
	for _, name := range names {
		m.ensureTab(name)
	}
	m.active = names[0]
	return m
}

func TestRailFitsEveryWidth(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m := railModel(t, width, railChannelNames()...)
		for _, line := range m.railLines() {
			if w := lipgloss.Width(line); w > width {
				t.Fatalf("at %d columns the rail is %d wide: %q", width, w, line)
			}
		}
	}
}

func TestRailShowsNameHintsAndTabMarkers(t *testing.T) {
	m := railModel(t, 120, "alfa", "bravo")
	m.tabs["bravo"].unreadCount = 3
	row := m.railLines()[0]
	for _, want := range []string{railName, newTranslator().t("tui.rail.hints", nil), "alfa", "bravo", unicodeGlyphs().selected, unicodeGlyphs().unread, "3"} {
		if !strings.Contains(row, want) {
			t.Fatalf("rail %q missing %q", row, want)
		}
	}
}

func TestRailMarksAFinishedTabWithACheck(t *testing.T) {
	m := railModel(t, 120, "alfa", "bravo")
	m.tabs["bravo"].finished = true
	if row := m.railLines()[0]; !strings.Contains(row, unicodeGlyphs().success) {
		t.Fatalf("a finished tab must carry the check mark: %q", row)
	}
}

func TestRailHasARuleUnderTheRow(t *testing.T) {
	m := railModel(t, 80, "alfa", "bravo")
	lines := m.railLines()
	if len(lines) != 2 {
		t.Fatalf("rail must be a row and a rule, got %d lines", len(lines))
	}
	if !strings.Contains(lines[1], unicodeGlyphs().ruleChar) {
		t.Fatalf("second line must be the rule: %q", lines[1])
	}
}

func TestRailKeepsTheActiveTabAtTenTabsAt60Columns(t *testing.T) {
	m := railModel(t, 60, railChannelNames()...)
	m.active = "golf"
	row := m.railLines()[0]
	if !strings.Contains(row, "golf") {
		t.Fatalf("the active tab must stay visible at 60 columns: %q", row)
	}
	if w := lipgloss.Width(row); w > 60 {
		t.Fatalf("rail is %d columns at 60: %q", w, row)
	}
}

func TestRailFoldReportsTheHiddenTabs(t *testing.T) {
	m := railModel(t, 40, railChannelNames()...)
	row := m.railLines()[0]
	if !strings.Contains(row, "+") {
		t.Fatalf("a folded rail must report its hidden tabs: %q", row)
	}
	if w := lipgloss.Width(row); w > 40 {
		t.Fatalf("rail is %d columns at 40: %q", w, row)
	}
	if !strings.Contains(row, "alfa") {
		t.Fatalf("the active tab must stay visible: %q", row)
	}
}

func TestRailKeepsTheActiveTabWhenTooNarrow(t *testing.T) {
	m := railModel(t, 12, "neublox", "herrscher", "docs-site")
	row := m.railLines()[0]
	if w := lipgloss.Width(row); w > 12 {
		t.Fatalf("rail is %d columns, want <= 12: %q", w, row)
	}
	if !strings.Contains(row, unicodeGlyphs().selected) || !strings.Contains(row, "neub") {
		t.Fatalf("rail %q dropped the active tab", row)
	}
}

func TestRailDropsTheNameBeforeTheTabs(t *testing.T) {
	m := railModel(t, 20, "alfa", "bravo")
	row := m.railLines()[0]
	if strings.Contains(row, railName) {
		t.Fatalf("a narrow rail must drop the name first: %q", row)
	}
	if !strings.Contains(row, "alfa") {
		t.Fatalf("a narrow rail must keep the tabs: %q", row)
	}
}

func TestShortenNameEndsWithAnEllipsis(t *testing.T) {
	if got := shortenName("neublox-session", 8); lipgloss.Width(got) > 8 || !strings.HasSuffix(got, "…") {
		t.Fatalf("shortenName returned %q", got)
	}
}
