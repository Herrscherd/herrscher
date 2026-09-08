package tui

import (
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/charmbracelet/x/ansi"
)

func strip(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	return out
}

func TestTodoPanelCountsAndMarks(t *testing.T) {
	items := []contracts.TodoItem{
		{Text: "lire capabilities.go", State: "done"},
		{Text: "ecrire le test", State: "done"},
		{Text: "brancher le panneau", State: "active"},
		{Text: "relire le diff", State: "pending"},
	}
	got := strip(todoPanel(items, 74))
	want := []string{
		"  ✓ todos 2/4",
		"  ✓ lire capabilities.go",
		"  ✓ ecrire le test",
		"  ◆ brancher le panneau",
		"  · relire le diff",
	}
	if len(got) != len(want) {
		t.Fatalf("panel drew %d lines: %q", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTodoPanelWindowsALongList(t *testing.T) {
	items := make([]contracts.TodoItem, 12)
	for i := range items {
		items[i] = contracts.TodoItem{Text: "tache", State: "pending"}
	}
	items[9].State = "active"
	got := strip(todoPanel(items, 74))
	if len(got) != todoPanelMax+2 {
		t.Fatalf("windowed panel drew %d lines: %q", len(got), got)
	}
	if !strings.Contains(got[len(got)-1], "+") {
		t.Fatalf("un panneau fenetre doit dire ce qu'il cache: %q", got)
	}
}

func TestTodoPanelIsEmptyWithoutItems(t *testing.T) {
	if got := todoPanel(nil, 74); got != nil {
		t.Fatalf("empty list drew %q", got)
	}
}

func TestSubagentPanelDrawsOneLinePerAgent(t *testing.T) {
	now := time.Now()
	agents := []liveAgent{
		{Subagent: contracts.Subagent{ID: "t1", Name: "explorer le hub", Kind: "Explore"}, since: now.Add(-18 * time.Second)},
		{Subagent: contracts.Subagent{ID: "t2", Name: "relire"}, since: now.Add(-3 * time.Second)},
	}
	got := strip(subagentPanel(agents, now, 74))
	if len(got) != 2 {
		t.Fatalf("panel drew %d lines: %q", len(got), got)
	}
	if got[0] != "  ◆ explorer le hub  Explore  18s" {
		t.Fatalf("line 0 = %q", got[0])
	}
	if got[1] != "  ◆ relire  3s" {
		t.Fatalf("line 1 = %q", got[1])
	}
}

func TestPanelsFallBackToTheirHeaderWhenNarrow(t *testing.T) {
	items := []contracts.TodoItem{{Text: "a", State: "active"}, {Text: "b", State: "pending"}}
	if got := strip(todoPanel(items, 40)); len(got) != 1 || got[0] != "  ✓ todos 0/2" {
		t.Fatalf("narrow panel = %q", got)
	}
}

func TestTheFrameCarriesThePanels(t *testing.T) {
	m := sizedTestModel(t, 74)
	tb := m.ensureTab("c1")
	m.active = "c1"
	tb.todos = []contracts.TodoItem{{Text: "brancher", State: "active"}}
	tb.agents = []liveAgent{{Subagent: contracts.Subagent{ID: "t1", Name: "explorer"}, since: time.Now()}}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "todos 0/1") || !strings.Contains(out, "explorer") {
		t.Fatalf("frame missing the panels:\n%s", out)
	}
}

func TestPanelRowsStayOnOneLine(t *testing.T) {
	got := strip(todoPanel([]contracts.TodoItem{{Text: "premiere\nseconde", State: "active"}}, 74))
	if len(got) != 2 {
		t.Fatalf("un todo multiligne doit rester une ligne: %q", got)
	}
	if got[1] != "  ◆ premiere seconde" {
		t.Fatalf("ligne = %q", got[1])
	}
	if got := strip(todoPanel([]contracts.TodoItem{{Text: "\x1b[2Jefface", State: "pending"}}, 74)); got[1] != "  · efface" {
		t.Fatalf("une sequence de controle doit etre retiree: %q", got)
	}
	agents := []liveAgent{{Subagent: contracts.Subagent{ID: "t1", Name: "a\nb"}, since: time.Now()}}
	if lines := strip(subagentPanel(agents, time.Now(), 74)); len(lines) != 1 {
		t.Fatalf("un agent multiligne doit rester une ligne: %q", lines)
	}
}

func TestTheWorkingHintSaysTheVerbOnce(t *testing.T) {
	m := sizedTestModel(t, 74)
	tb := m.ensureTab("c1")
	tb.busy = true
	tb.startedAt = time.Now()
	if hint := ansi.Strip(m.spinnerHint(tb)); strings.Contains(hint, "… …") || strings.Contains(hint, workingVerb+" …") {
		t.Fatalf("hint = %q", hint)
	}
}
