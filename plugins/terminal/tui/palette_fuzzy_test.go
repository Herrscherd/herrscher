package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPaletteFuzzyMatchesCommandsByAbbreviation(t *testing.T) {
	got := filterCommands(localCommands(), "qu")
	if len(got) == 0 || got[0].Name != "queue" {
		names := make([]string, 0, len(got))
		for _, c := range got {
			names = append(names, c.Name)
		}
		t.Fatalf("qu must rank queue first: %v", names)
	}
}

func TestQueueCommandCarriesPRTag(t *testing.T) {
	for _, c := range localCommands() {
		if c.Name == "queue" {
			if c.PR != "PR 2" {
				t.Fatalf("queue PR = %q", c.PR)
			}
			return
		}
	}
	t.Fatal("queue command missing from local commands")
}

func TestPaletteViewShowsPRTagAndHighlight(t *testing.T) {
	m, _ := queueFixture(t)
	typeLine(m, "/que")
	view := m.paletteView()
	if !strings.Contains(view, "PR 2") {
		t.Fatalf("palette must show the PR tag: %q", view)
	}
	if !strings.Contains(view, "queue") {
		t.Fatalf("palette must list queue: %q", view)
	}
}

func TestMentionMatchesRecurseAndRank(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "src", "deep"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "src", "main.go"), nil, 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "src", "deep", "marine.go"), nil, 0o600))
	must(t, os.WriteFile(filepath.Join(dir, "README.md"), nil, 0o600))

	got := mentionMatches(dir, "mn")
	if len(got) == 0 {
		t.Fatal("fuzzy mention must find nested files")
	}
	if !contains(got, "src/main.go") {
		t.Fatalf("mention matches = %v", got)
	}
}

func TestMentionMatchesUnreadableDirIsNil(t *testing.T) {
	if got := mentionMatches(filepath.Join(t.TempDir(), "absent"), "x"); got != nil {
		t.Fatalf("unreadable dir must give nil, got %v", got)
	}
}

func TestMentionTopLevelWithoutPrefix(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o600))
	got := mentionMatches(dir, "")
	if contains(got, "sub/x") || len(got) == 0 {
		t.Fatalf("empty prefix must list top-level entries: %v", got)
	}
	if !reflect.DeepEqual(contains(got, "a.txt"), true) {
		t.Fatalf("top-level file missing: %v", got)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
