package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestPaletteInlineRenderSelectedRow(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("/")
	m.palIdx = 1
	out := m.paletteView()
	if !strings.Contains(out, unicodeGlyphs().cursor+" /clear") {
		t.Fatalf("selected row must be prefixed %q in accent: %q", unicodeGlyphs().cursor, out)
	}
	for _, box := range []string{"╭", "╮", "╰", "─", "│"} {
		if strings.Contains(out, box) {
			t.Fatalf("palette must be borderless, found %q: %q", box, out)
		}
	}
}

func TestPaletteWindowFollowsTheSelection(t *testing.T) {
	for _, tc := range []struct{ n, sel, wantStart, wantEnd int }{
		{n: 4, sel: 2, wantStart: 0, wantEnd: 4},
		{n: 30, sel: 0, wantStart: 0, wantEnd: 6},
		{n: 30, sel: 5, wantStart: 0, wantEnd: 6},
		{n: 30, sel: 6, wantStart: 1, wantEnd: 7},
		{n: 30, sel: 29, wantStart: 24, wantEnd: 30},
	} {
		start, end := paletteWindow(tc.n, tc.sel, 6)
		if start != tc.wantStart || end != tc.wantEnd {
			t.Fatalf("n=%d sel=%d: got [%d,%d), want [%d,%d)", tc.n, tc.sel, start, end, tc.wantStart, tc.wantEnd)
		}
		if tc.sel < start || tc.sel >= end {
			t.Fatalf("n=%d sel=%d fell outside the drawn window [%d,%d)", tc.n, tc.sel, start, end)
		}
	}
}

func TestPaletteViewShowsALateSelection(t *testing.T) {
	m := newTestModel()
	m.cmds = nil
	for i := range 30 {
		m.cmds = append(m.cmds, CommandSpec{Name: fmt.Sprintf("verb%02d", i)})
	}
	m.input.SetValue("/")
	m.palIdx = 27
	out := m.paletteView()
	if !strings.Contains(out, unicodeGlyphs().cursor+" /verb27") {
		t.Fatalf("a late selection must be drawn: %q", out)
	}
	if strings.Contains(out, "/verb00") {
		t.Fatalf("the window must have scrolled past the head: %q", out)
	}
	if !strings.Contains(out, "28/30") {
		t.Fatalf("a windowed palette must say where the cursor is: %q", out)
	}
	if h := lipgloss.Height(out); h != m.overlayRows()+1 {
		t.Fatalf("palette height %d, want %d rows plus the counter", h, m.overlayRows()+1)
	}
}

func TestLocalCommandsAreNeverDispatched(t *testing.T) {
	readVerbs := []string{verbHealth}
	for _, c := range localCommands(newTranslator()) {
		be := &fakeBackend{}
		be.sessions = []contracts.SessionInfo{{Name: "avant", ChannelID: "c1"}}
		m := newModel(be)
		m.ensureTab("c1")
		m.active = "c1"
		m.syncTabs()
		m.input.SetValue("/" + c.Name)
		runCmd(m.handleEnter())
		for _, argv := range be.dispatched {
			if argv[0] == c.Name && !slices.Contains(readVerbs, c.Name) {
				t.Errorf("/%s is advertised as local but was dispatched: %v", c.Name, argv)
			}
		}
	}
}

func TestMergeCommandsKeepsTheLocalOnes(t *testing.T) {
	got := mergeCommands(localCommands(newTranslator()), []CommandSpec{
		{Name: "session create"},
		{Name: "help", Desc: "the daemon's own help"},
	})
	have := make(map[string]string)
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
