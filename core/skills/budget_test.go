package skills

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/contextwindow"
)

type testNotices struct{}

func (testNotices) MenuMore(hidden int) string { return fmt.Sprintf("MORE %d", hidden) }

func (testNotices) ExpansionCut(name, path string) string { return "CUT " + name + " " + path }

func TestMenuUnderBudgetIsUnchanged(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "pdf-fill", "name: pdf-fill\ndescription: fill PDFs\n", "A\n")
	writeSkill(t, root, "web", "name: web\ndescription: browse the web\n", "B\n")
	e := NewEngine([]string{root})
	unbounded := e.Menu("fill a pdf", testNotices{})

	e.SetBudget(Budget{Menu: 10_000})
	if got := e.Menu("fill a pdf", testNotices{}); got != unbounded {
		t.Fatalf("menu under budget must be unchanged:\nwant:\n%s\ngot:\n%s", unbounded, got)
	}
	if strings.Contains(unbounded, "MORE") {
		t.Fatalf("unbounded menu must not carry a hidden notice:\n%s", unbounded)
	}
}

func TestMenuBudgetOffListsEverySkill(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a-one", "b-two", "c-three", "d-four"} {
		writeSkill(t, root, name, "name: "+name+"\ndescription: does "+name+" things\n", "X\n")
	}
	e := NewEngine([]string{root})
	e.SetBudget(Budget{})
	menu := e.Menu("a-one", testNotices{})
	for _, name := range []string{"a-one", "b-two", "c-three", "d-four"} {
		if !strings.Contains(menu, "- "+name+": ") {
			t.Fatalf("budget off must list %s:\n%s", name, menu)
		}
	}
	if strings.Contains(menu, "MORE") {
		t.Fatalf("budget off must not hide anything:\n%s", menu)
	}
}

func TestMenuOverBudgetKeepsTopKByRelevance(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "deploy", "name: deploy\ndescription: ship the kubernetes cluster\n", "A\n")
	writeSkill(t, root, "rollout", "name: rollout\ndescription: kubernetes rollout checks\n", "B\n")
	writeSkill(t, root, "zebra", "name: zebra\ndescription: unrelated herding notes\n", "C\n")
	writeSkill(t, root, "yak", "name: yak\ndescription: unrelated wool notes\n", "D\n")
	e := NewEngine([]string{root})
	fitTwo := contextwindow.EstimateTokens(e.renderMenu(map[string]bool{"deploy": true, "rollout": true}, 2, testNotices{}))
	e.SetBudget(Budget{Menu: fitTwo})

	got := e.Menu("deploy the kubernetes rollout", testNotices{})
	want := e.renderMenu(map[string]bool{"deploy": true, "rollout": true}, 2, testNotices{})
	if got != want {
		t.Fatalf("over budget must keep the two most relevant skills:\nwant:\n%s\ngot:\n%s", want, got)
	}
	if !strings.Contains(got, "MORE 2") {
		t.Fatalf("hidden skills must be counted in the notice:\n%s", got)
	}
}

func TestMenuNeverDropsAnActiveSkill(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "zeta", "name: zeta\ndescription: unrelated herding notes\n", "Z\n")
	writeSkill(t, root, "deploy", "name: deploy\ndescription: ship the kubernetes cluster\n", "D\n")
	e := NewEngine([]string{root})
	e.Detect("<use-skill>zeta</use-skill>")
	e.SetBudget(Budget{Menu: 1})

	menu := e.Menu("kubernetes deploy", testNotices{})
	if !strings.Contains(menu, "- zeta: unrelated herding notes") {
		t.Fatalf("active skill must stay listed even when over budget:\n%s", menu)
	}
	if strings.Contains(menu, "- deploy: ") {
		t.Fatalf("an inactive skill must not fit a budget of one token:\n%s", menu)
	}
}

func TestMenuRankingIsDeterministic(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeSkill(t, first, "mango", "name: mango\ndescription: fruit notes\n", "M\n")
	writeSkill(t, second, "apple", "name: apple\ndescription: fruit notes\n", "A\n")
	writeSkill(t, second, "kiwi", "name: kiwi\ndescription: fruit notes\n", "K\n")
	roots := [][]string{{first, second}, {second, first}}

	var menus []string
	for _, order := range roots {
		e := NewEngine(order)
		e.SetBudget(Budget{Menu: menuTokensShowing(e, "fruit", 1)})
		for range 3 {
			menus = append(menus, e.Menu("fruit", testNotices{}))
		}
	}
	for _, menu := range menus[1:] {
		if menu != menus[0] {
			t.Fatalf("ranking must not depend on repetition or discovery order:\n%s\nvs\n%s", menus[0], menu)
		}
	}
	if !strings.Contains(menus[0], "- apple: fruit notes") || strings.Contains(menus[0], "- kiwi: ") {
		t.Fatalf("ties must break by name, keeping apple first:\n%s", menus[0])
	}
}

func TestRelevanceCountsNameMatchesAboveDescriptionMatches(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "deploy", "name: deploy\ndescription: ship things\n", "A\n")
	writeSkill(t, root, "notes", "name: notes\ndescription: deploy things\n", "B\n")
	e := NewEngine([]string{root})
	e.SetBudget(Budget{Menu: menuTokensShowing(e, "deploy", 1)})
	menu := e.Menu("deploy", testNotices{})
	if !strings.Contains(menu, "- deploy: ship things") || strings.Contains(menu, "- notes: ") {
		t.Fatalf("a name match must outrank a description match:\n%s", menu)
	}
}

func TestExpansionsUnderBudgetIsUnchanged(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "name: alpha\ndescription: a\n", "ALPHA BODY\n")
	writeSkill(t, root, "beta", "name: beta\ndescription: b\n", "BETA BODY\n")
	e := NewEngine([]string{root})
	e.Detect("<use-skill>alpha</use-skill> <use-skill>beta</use-skill>")
	unbounded := e.Expansions(testNotices{})

	e.SetBudget(Budget{Expansions: 10_000})
	if got := e.Expansions(testNotices{}); got != unbounded {
		t.Fatalf("expansions under budget must be unchanged:\nwant:\n%s\ngot:\n%s", unbounded, got)
	}
	if strings.Contains(unbounded, "CUT") {
		t.Fatalf("unbounded expansions must not cut:\n%s", unbounded)
	}
}

func TestExpansionsOverBudgetCutsAndNamesEveryCutSkill(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("aaaa\n", 40)
	writeSkill(t, root, "alpha", "name: alpha\ndescription: a\n", long)
	writeSkill(t, root, "beta", "name: beta\ndescription: b\n", "BETA BODY\n")
	e := NewEngine([]string{root})
	e.Detect("<use-skill>alpha</use-skill> <use-skill>beta</use-skill>")
	e.SetBudget(Budget{Expansions: 20})

	got := e.Expansions(testNotices{})
	for _, want := range []string{"CUT alpha " + filepath.Join(root, "alpha", "SKILL.md"), "CUT beta " + filepath.Join(root, "beta", "SKILL.md")} {
		if !strings.Contains(got, want) {
			t.Fatalf("a cut skill must be named with its file:\nwant %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "BETA BODY") {
		t.Fatalf("beta's body must not leak past the budget:\n%s", got)
	}
	if kept := strings.Count(got, "aaaa\n"); kept == 0 || kept > 16 {
		t.Fatalf("alpha must keep a prefix within 20 tokens, kept %d lines:\n%s", kept, got)
	}
}

func TestFitTokensCutsAtLineAndRuneBoundaries(t *testing.T) {
	if got := fitTokens("first line\nsecond line", 0); got != "" {
		t.Fatalf("no tokens left must keep nothing, got %q", got)
	}
	if got := fitTokens("short", 10); got != "short" {
		t.Fatalf("a body within the budget is kept whole, got %q", got)
	}
	if got := fitTokens("first line\nsecond line that runs on", 4); got != "first line" {
		t.Fatalf("a cut must land on the last whole line, got %q", got)
	}
	if got := fitTokens("aéé", 1); got != "aé" {
		t.Fatalf("a cut must not split a rune, got %q", got)
	}
}

func menuTokensShowing(e *Engine, message string, count int) int {
	shown := make(map[string]bool, count)
	for _, name := range e.rankedInactive(message)[:count] {
		shown[name] = true
	}
	return contextwindow.EstimateTokens(e.renderMenu(shown, len(e.order)-count, testNotices{}))
}
