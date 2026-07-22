package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Herrscherd/herrscher/core/skills"
)

// TestSkillsPanelRendersRows checks the panel renders a dim header and one row
// per skill (name — description), the selected row prefixed ❯, with no border.
func TestSkillsPanelRendersRows(t *testing.T) {
	m := newTestModel()
	m.skillsRows = []skills.Skill{
		{Name: "pdf-fill", Description: "fill PDFs"},
		{Name: "web", Description: "browse"},
	}
	m.skillsOpen = true

	out := m.skillsView()
	if !strings.Contains(out, glyphCursor+" pdf-fill — fill PDFs") {
		t.Fatalf("selected row must be prefixed and show name — desc: %q", out)
	}
	if !strings.Contains(out, "web — browse") {
		t.Fatalf("second skill must be listed: %q", out)
	}
	for _, box := range []string{"╭", "╮", "╰", "│"} {
		if strings.Contains(out, box) {
			t.Fatalf("panel must be borderless, found %q: %q", box, out)
		}
	}
}

// TestSkillsPanelEmpty checks the panel shows a friendly empty line when no
// skills are discovered.
func TestSkillsPanelEmpty(t *testing.T) {
	m := newTestModel()
	m.skillsOpen = true
	if !strings.Contains(m.skillsView(), "no skills found") {
		t.Fatalf("empty panel must explain there are no skills: %q", m.skillsView())
	}
}

// TestSkillsPanelEscCloses checks Esc dismisses the modal panel.
func TestSkillsPanelEscCloses(t *testing.T) {
	m := newTestModel()
	m.skillsRows = []skills.Skill{{Name: "one", Description: "first"}}
	m.skillsOpen = true
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.skillsOpen {
		t.Fatal("Esc must close the skills panel")
	}
}

// TestSkillsPanelNavClamps checks the selection never leaves the row set.
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
