package tui

import (
	"strings"
	"testing"
)

// TestPaletteInlineRenderSelectedRow checks the palette renders inline (no border
// box) with the selected row prefixed ❯ in the warm accent.
func TestPaletteInlineRenderSelectedRow(t *testing.T) {
	m := newTestModel() // nil backend → seeded with defaultCommands
	m.input.SetValue("/")
	m.palIdx = 0
	out := m.paletteView()
	if !strings.Contains(out, glyphCursor+" /clear") {
		t.Fatalf("selected row must be prefixed %q in accent: %q", glyphCursor, out)
	}
	for _, box := range []string{"╭", "╮", "╰", "─", "│"} {
		if strings.Contains(out, box) {
			t.Fatalf("palette must be borderless, found %q: %q", box, out)
		}
	}
}

// TestDefaultCommandsIncludeClaudeSet checks the Claude command set is seeded.
func TestDefaultCommandsIncludeClaudeSet(t *testing.T) {
	have := map[string]bool{}
	for _, c := range defaultCommands() {
		have[c.Name] = true
	}
	for _, want := range []string{"clear", "help", "session switch", "session create", "resume"} {
		if !have[want] {
			t.Fatalf("default command set missing %q: %+v", want, defaultCommands())
		}
	}
}

func TestFilterCommands(t *testing.T) {
	cmds := []CommandSpec{
		{Name: "session create"},
		{Name: "session list"},
		{Name: "agent create"},
	}
	cases := []struct {
		query string
		want  []string
	}{
		{"", []string{"session create", "session list", "agent create"}},
		{"session", []string{"session create", "session list"}},
		{"create", []string{"session create", "agent create"}},
		{"nope", nil},
		// Once the query advances into a command's arguments, that command must
		// keep matching so the palette does not misreport a valid command as "no match".
		{"session create --name foo", []string{"session create"}},
	}
	for _, tc := range cases {
		got := filterCommands(cmds, tc.query)
		if len(got) != len(tc.want) {
			t.Fatalf("query %q: got %d matches, want %d (%+v)", tc.query, len(got), len(tc.want), got)
		}
		for i, name := range tc.want {
			if got[i].Name != name {
				t.Fatalf("query %q: match %d = %q, want %q", tc.query, i, got[i].Name, name)
			}
		}
	}
}
