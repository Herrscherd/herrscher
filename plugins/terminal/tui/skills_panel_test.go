package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Herrscherd/herrscher/core/skills"
)

func TestSkillsPanelRendersRows(t *testing.T) {
	m := newTestModel()
	m.skillsRows = []skills.Skill{
		{Name: "pdf-fill", Description: "fill PDFs"},
		{Name: "web", Description: "browse"},
	}
	m.skillsOpen = true

	out := m.skillsView()
	if !strings.Contains(out, unicodeGlyphs().cursor+" pdf-fill") || !strings.Contains(out, "fill PDFs") {
		t.Fatalf("selected row must be prefixed and show its name and description: %q", out)
	}
	if !strings.Contains(out, "web") || !strings.Contains(out, "browse") {
		t.Fatalf("second skill must be listed: %q", out)
	}
	for _, box := range []string{"╭", "╮", "╰", "│"} {
		if strings.Contains(out, box) {
			t.Fatalf("panel must be borderless, found %q: %q", box, out)
		}
	}
}

func TestSkillsPanelEmpty(t *testing.T) {
	m := newTestModel()
	m.skillsOpen = true
	if !strings.Contains(m.skillsView(), "no skill in") {
		t.Fatalf("empty panel must explain there are no skills: %q", m.skillsView())
	}
}

func TestSkillsPanelEscCloses(t *testing.T) {
	m := newTestModel()
	m.skillsRows = []skills.Skill{{Name: "one", Description: "first"}}
	m.skillsOpen = true
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.skillsOpen {
		t.Fatal("Esc must close the skills panel")
	}
}

func TestSkillsPanelNavClamps(t *testing.T) {
	m := newTestModel()
	m.skillsRows = []skills.Skill{{Name: "one"}, {Name: "two"}}
	m.skillsOpen = true
	m.moveSkills(-1)
	if m.skillsIdx != 0 {
		t.Fatalf("up at top must stay at 0, got %d", m.skillsIdx)
	}
	m.moveSkills(5)
	if m.skillsIdx != 1 {
		t.Fatalf("down past end must clamp to last, got %d", m.skillsIdx)
	}
}

func TestSkillPanelSeesTheRootsTheMenuScans(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("HERRSCHER_STATE_DIR", stateDir)
	extraRoot := t.TempDir()
	writeSkill(t, filepath.Join(extraRoot, "extra"), "extra")
	config := fmt.Sprintf(`{"skills":{"roots":[%q]}}`, extraRoot)
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	writeSkill(t, filepath.Join(cwd, ".claude", "skills", "proj"), "proj")

	roots := skillPanelRoots(cwd)
	want := skills.Roots(cwd, []string{extraRoot})
	if !slices.Equal(roots, want) {
		t.Fatalf("the panel must scan exactly the menu's roots:\npanel %v\nmenu  %v", roots, want)
	}
	listed := make(map[string]bool)
	for _, row := range skills.Discover(roots) {
		listed[row.Name] = true
	}
	for _, name := range []string{"proj", "extra"} {
		if !listed[name] {
			t.Fatalf("the panel must list the skill %q the menu can hide, roots %v", name, roots)
		}
	}
}

func writeSkill(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: a " + name + " skill\n---\nBODY\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
