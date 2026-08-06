package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestPaletteInlineRenderSelectedRow checks the palette renders inline (no border
// box) with the selected row prefixed ❯ in the warm accent.
func TestPaletteInlineRenderSelectedRow(t *testing.T) {
	m := newTestModel() // nil backend → seeded with localCommands
	m.input.SetValue("/")
	m.palIdx = 1 // /clear: the row after the leading /help
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

// TestPaletteWindowFollowsTheSelection is the regression for a palette that
// truncated at paletteMax from the head: every command past the sixth was
// unreachable with ↑↓, so a daemon advertising thirty verbs showed six.
func TestPaletteWindowFollowsTheSelection(t *testing.T) {
	for _, tc := range []struct{ n, sel, wantStart, wantEnd int }{
		{n: 4, sel: 2, wantStart: 0, wantEnd: 4},     // fits: no window at all
		{n: 30, sel: 0, wantStart: 0, wantEnd: 6},    // top: the first rows stay put
		{n: 30, sel: 5, wantStart: 0, wantEnd: 6},    // still on the last visible row
		{n: 30, sel: 6, wantStart: 1, wantEnd: 7},    // one past it: slide by one
		{n: 30, sel: 29, wantStart: 24, wantEnd: 30}, // bottom: clamped, never past the end
	} {
		start, end := paletteWindow(tc.n, tc.sel, paletteMax)
		if start != tc.wantStart || end != tc.wantEnd {
			t.Fatalf("n=%d sel=%d: got [%d,%d), want [%d,%d)", tc.n, tc.sel, start, end, tc.wantStart, tc.wantEnd)
		}
		if tc.sel < start || tc.sel >= end {
			t.Fatalf("n=%d sel=%d fell outside the drawn window [%d,%d)", tc.n, tc.sel, start, end)
		}
	}
}

// TestPaletteViewShowsALateSelection: the window has to reach the view, not just
// the helper — the selected row must be drawn however far down the list it is.
func TestPaletteViewShowsALateSelection(t *testing.T) {
	m := newTestModel()
	m.cmds = nil
	for i := 0; i < 30; i++ {
		m.cmds = append(m.cmds, CommandSpec{Name: fmt.Sprintf("verb%02d", i)})
	}
	m.input.SetValue("/")
	m.palIdx = 27
	out := m.paletteView()
	if !strings.Contains(out, glyphCursor+" /verb27") {
		t.Fatalf("a late selection must be drawn: %q", out)
	}
	if strings.Contains(out, "/verb00") {
		t.Fatalf("the window must have scrolled past the head: %q", out)
	}
	if !strings.Contains(out, "28/30") {
		t.Fatalf("a windowed palette must say where the cursor is: %q", out)
	}
	if h := lipgloss.Height(out); h != paletteMax+1 {
		t.Fatalf("palette height %d, want %d rows plus the counter", h, paletteMax+1)
	}
}

// TestLocalCommandsAreAllHandledLocally: every verb in localCommands claims to
// need no daemon, so each must be intercepted by handleEnter. One that is only
// advertised would reach dispatchCmd and come back "unknown command".
func TestLocalCommandsAreAllHandledLocally(t *testing.T) {
	src, err := os.ReadFile("tui.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range localCommands() {
		verb := strings.Fields(c.Name)[0]
		if !strings.Contains(string(src), `args[0] == "`+verb+`"`) {
			t.Errorf("/%s is advertised as local but handleEnter never intercepts %q", c.Name, verb)
		}
	}
}

// TestMergeCommandsKeepsTheLocalOnes: a backend advertising commands used to
// replace the palette outright, so connecting to a daemon cost the operator
// /help, /clear and every other overlay.
func TestMergeCommandsKeepsTheLocalOnes(t *testing.T) {
	got := mergeCommands(localCommands(), []CommandSpec{
		{Name: "session create"},
		{Name: "help", Desc: "the daemon's own help"},
	})
	have := map[string]string{}
	for _, c := range got {
		if _, dup := have[c.Name]; dup {
			t.Fatalf("duplicate row %q", c.Name)
		}
		have[c.Name] = c.Desc
	}
	for _, want := range []string{"clear", "help", "session switch", "resume", "session create"} {
		if _, ok := have[want]; !ok {
			t.Fatalf("merged palette missing %q: %+v", want, got)
		}
	}
	if have["help"] == "the daemon's own help" {
		t.Error("a daemon command must not shadow the local one it collides with")
	}
}

// TestParseCommandSpecsFiltersAndFlags checks the daemon's answer becomes rows
// the CLI parser would actually accept: required params only, in --flag form.
func TestParseCommandSpecsFiltersAndFlags(t *testing.T) {
	raw := []byte(`[
		{"path":["session","create"],"help":"start","params":[{"Name":"name","Required":true},{"Name":"repo","Required":false}]},
		{"path":["service","restart"],"help":"nope"}
	]`)
	got, err := ParseCommandSpecs(raw, func(v string) bool { return v == "session" })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v, want only the session row", got)
	}
	if got[0].Name != "session create" || got[0].Args != "--name <name>" || got[0].Desc != "start" {
		t.Fatalf("row = %+v", got[0])
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
