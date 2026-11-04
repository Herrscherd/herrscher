package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func sidebarModel(t *testing.T, width int) *model {
	t.Helper()
	m := sizedTestModel(t, width)
	m.ensureTab("alfa")
	m.active = "alfa"
	tb := m.tabs["alfa"]
	tb.todos = []contracts.TodoItem{
		{Text: "ecrire le rail", State: stateDone},
		{Text: "tester la colonne", State: "active"},
	}
	tb.applySubagent(&contracts.Subagent{ID: "a1", Name: "explore", Kind: "explore", State: "running"})
	tb.files = []string{"plugins/terminal/tui/rail.go"}
	return m
}

func TestSidebarAppearsFromTheColumnThreshold(t *testing.T) {
	if sizedTestModelVisible(t, sidebarAutoWidth-1) {
		t.Fatalf("the sidebar must stay hidden under %d columns", sidebarAutoWidth)
	}
	if !sizedTestModelVisible(t, sidebarAutoWidth) {
		t.Fatalf("the sidebar must show from %d columns", sidebarAutoWidth)
	}
}

func sizedTestModelVisible(t *testing.T, width int) bool {
	t.Helper()
	return sizedTestModel(t, width).sidebarVisible()
}

func TestSidebarShowsItsSectionsWhenVisible(t *testing.T) {
	view := sidebarModel(t, 120).View()
	for _, want := range []string{"SESSION", "TODOS 1/2", "SOUS-AGENTS", "FICHIERS", "CONTEXTE", unicodeGlyphs().filet} {
		if !strings.Contains(view, want) {
			t.Fatalf("the sidebar must show %q at 120 columns", want)
		}
	}
}

func TestSidebarStaysHiddenBelowTheThreshold(t *testing.T) {
	for _, width := range []int{60, 80} {
		view := sidebarModel(t, width).View()
		if strings.Contains(view, "SOUS-AGENTS") || strings.Contains(view, "Todos") {
			t.Fatalf("at %d columns the sidebar must stay hidden", width)
		}
	}
}

func TestSidebarFoldsItsBottomSectionsBelowTwentyFourRows(t *testing.T) {
	m := sidebarModel(t, 120)
	if !strings.Contains(m.View(), "explore") {
		t.Fatal("at 24 rows the sub-agent row must be listed")
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	view := m.View()
	if strings.Contains(view, "explore") {
		t.Fatal("below 24 rows the sub-agent section must fold to its summary line")
	}
	if !strings.Contains(view, "SOUS-AGENTS") {
		t.Fatal("the folded sub-agent section must keep its summary line")
	}
}

func TestViewNeverExceedsTheScreenWidth(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		for _, line := range strings.Split(sidebarModel(t, width).View(), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Fatalf("at %d columns a line is %d wide: %q", width, w, line)
			}
		}
	}
}

func TestCtrlBTogglesTheSidebar(t *testing.T) {
	m := sidebarModel(t, 120)
	if !m.sidebarVisible() {
		t.Fatal("the sidebar must start visible at 120 columns")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if m.sidebarVisible() {
		t.Fatal("ctrl+b must hide the visible sidebar")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if !m.sidebarVisible() {
		t.Fatal("ctrl+b must show the hidden sidebar again")
	}
}

func TestParseNumstatReadsAddedAndRemovedLines(t *testing.T) {
	stats := parseNumstat("3\t1\trail.go\n-\t-\tlogo.png\n")
	if got := stats["rail.go"]; got != (lineStat{added: 3, removed: 1}) {
		t.Fatalf("rail.go stat = %+v", got)
	}
	if _, ok := stats["logo.png"]; ok {
		t.Fatal("a binary file has no line count and must be skipped")
	}
}

func TestBackgroundSectionAppearsOnlyWithTasks(t *testing.T) {
	m := sidebarModel(t, 120)
	if strings.Contains(m.View(), "DE FOND") {
		t.Fatal("the background section must stay hidden without tasks")
	}
	m.tabs[m.active].background = []backgroundTask{{ID: "b1", Name: "npm run build", State: "running"}}
	view := m.View()
	if !strings.Contains(view, "DE FOND") || !strings.Contains(view, "npm run build") {
		t.Fatal("the background section must list its tasks")
	}
}

func TestCtrlTOpensTheTabPicker(t *testing.T) {
	m := sidebarModel(t, 120)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if !m.switchOpen {
		t.Fatal("ctrl+t must open the tab picker")
	}
}

func TestGitStateIsReadFromTheCacheOnly(t *testing.T) {
	m := sidebarModel(t, 120)
	dir := t.TempDir()
	if got := m.gitStateOf(dir); got.branch != "" {
		t.Fatalf("an unrequested directory must read empty, got %+v", got)
	}
	m.storeGit(gitRefreshMsg{dir: dir, snap: gitSnapshot{branch: "tui-v2/layout"}})
	if got := m.gitStateOf(dir); got.branch != "tui-v2/layout" {
		t.Fatalf("the stored snapshot must be read back, got %+v", got)
	}
}

func TestGitRefreshIsThrottled(t *testing.T) {
	m := sidebarModel(t, 120)
	dir := t.TempDir()
	if m.requestGit(dir, false) == nil {
		t.Fatal("the first request must start a refresh")
	}
	if m.requestGit(dir, false) != nil {
		t.Fatal("a second request within the refresh window must be skipped")
	}
	if m.requestGit(dir, true) == nil {
		t.Fatal("a forced request must refresh even inside the window")
	}
}

func TestCountWordAgreesInNumber(t *testing.T) {
	cases := map[int]string{0: "0 actif", 1: "1 actif", 2: "2 actifs"}
	for count, want := range cases {
		if got := countWord(count, "actif", "actifs"); got != want {
			t.Errorf("countWord(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestSidebarContextGaugeHasSixteenCellsWithoutBrackets(t *testing.T) {
	m := sidebarModel(t, 120)
	tb := m.tabs["alfa"]
	tb.ctxTokens = 82_000
	info := contracts.SessionInfo{Vendor: "claude"}
	lines := m.contextSection(tb, info, 40, false)
	gaugeLine := ansi.Strip(lines[1])
	if cells := strings.Count(gaugeLine, unicodeGlyphs().gaugeFull); cells != sidebarGaugeCells {
		t.Fatalf("the sidebar gauge must have %d cells, got %d in %q", sidebarGaugeCells, cells, gaugeLine)
	}
	if strings.ContainsAny(gaugeLine, "[]") {
		t.Fatalf("the sidebar gauge must not be bracketed: %q", gaugeLine)
	}
	if !strings.Contains(ansi.Strip(lines[2]), "compacte seul à 92%") {
		t.Fatalf("the sidebar must say compaction happens at 92%%: %q", ansi.Strip(lines[2]))
	}
}
