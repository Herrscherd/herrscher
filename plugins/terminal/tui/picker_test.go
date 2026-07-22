package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestPermissionMenuRendersClaudeShape checks the permission menu renders the
// framing header plus numbered options, the first prefixed ❯, the last tagged
// (esc), with no border box.
func TestPermissionMenuRendersClaudeShape(t *testing.T) {
	m := newTestModel()
	m.openChoice(&PendingChoice{
		Question: "run Bash(rm -rf build/)",
		Options: []ChoiceOption{
			{Label: "Yes", Value: "yes"},
			{Label: "Yes, don't ask again", Value: "always"},
			{Label: "No", Value: "no"},
		},
	})
	out := m.choiceView()
	if !strings.Contains(out, "run Bash(rm -rf build/)") {
		t.Fatalf("menu must frame the action: %q", out)
	}
	if !strings.Contains(out, glyphCursor+" 1. Yes") {
		t.Fatalf("selected row must be %q 1. Yes: %q", glyphCursor, out)
	}
	if !strings.Contains(out, "2. Yes, don't ask again") {
		t.Fatalf("second option must be numbered: %q", out)
	}
	if !strings.Contains(out, "3. No  (esc)") {
		t.Fatalf("final deny row must be tagged (esc): %q", out)
	}
	for _, box := range []string{"╭", "╮", "╰", "│"} {
		if strings.Contains(out, box) {
			t.Fatalf("permission menu must be borderless, found %q: %q", box, out)
		}
	}
}

// TestPermissionMenuEnterPicksValue checks that Enter on a selection submits that
// option's value back to the active session as a pick and dismisses the menu.
func TestPermissionMenuEnterPicksValue(t *testing.T) {
	f := &fakeBackend{}
	m := newModel(f)
	m.ensureTab("a")
	m.active = "a"
	m.ready = true
	m.openChoice(&PendingChoice{
		Question: "run Bash(rm -rf build/)",
		Channel:  "a",
		Options: []ChoiceOption{
			{Label: "Yes", Value: "yes"},
			{Label: "No", Value: "no"},
		},
	})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.choice != nil {
		t.Fatal("menu must dismiss after a selection")
	}
	if len(f.submitted) != 1 || f.submitted[0].text != "yes" {
		t.Fatalf("Enter must submit the selected value: %+v", f.submitted)
	}
}

// TestPermissionMenuEscDenies checks Esc selects the final (deny) option and
// submits it.
func TestPermissionMenuEscDenies(t *testing.T) {
	f := &fakeBackend{}
	m := newModel(f)
	m.ensureTab("a")
	m.active = "a"
	m.ready = true
	m.openChoice(&PendingChoice{
		Question: "run Bash(rm -rf build/)",
		Channel:  "a",
		Options: []ChoiceOption{
			{Label: "Yes", Value: "yes"},
			{Label: "No", Value: "no"},
		},
	})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if len(f.submitted) != 1 || f.submitted[0].text != "no" {
		t.Fatalf("Esc must submit the final deny value: %+v", f.submitted)
	}
}
