package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakePluginSeam stands in for the host's plugin management: it answers from
// canned data and records every call that would touch the composition, so a test
// can assert that an abort wrote nothing.
type fakePluginSeam struct {
	rows      []PluginRow
	versions  []string
	findings  []string
	output    string
	applyErr  error
	applied   []string
	restored  int
	listCalls int
}

func (f *fakePluginSeam) List(context.Context) ([]PluginRow, error) {
	f.listCalls++
	return f.rows, nil
}

func (f *fakePluginSeam) Versions(context.Context, string) ([]string, error) {
	return f.versions, nil
}

func (f *fakePluginSeam) Findings(context.Context, string, string) []string { return f.findings }

func (f *fakePluginSeam) Apply(ctx context.Context, action PluginAction, module, version string) (string, error) {
	f.applied = append(f.applied, string(action)+" "+module+"@"+version)
	return f.output, f.applyErr
}

func (f *fakePluginSeam) Restore(context.Context) error { f.restored++; return nil }

// openPluginsScreen opens the screen on a seam and delivers the listing.
func openPluginsScreen(t *testing.T, f *fakePluginSeam) *model {
	t.Helper()
	m := newTestModel()
	m.SetPluginSeam(f)
	m.Update(runCmd(m.openPlugins()))
	return m
}

func testRows() []PluginRow {
	return []PluginRow{
		{Module: "mod/a", Installed: "v1.0.0", Latest: "v1.1.0"},
		{Module: "mod/b", Installed: "v2.0.0", Latest: "v2.0.0", Pinned: true},
	}
}

func TestPluginsScreenListsWhatTheSeamReports(t *testing.T) {
	m := openPluginsScreen(t, &fakePluginSeam{rows: testRows()})
	out := m.pluginsView()
	if !strings.Contains(out, "mod/a") || !strings.Contains(out, "v1.1.0") {
		t.Fatalf("screen must list each module with its versions: %q", out)
	}
	if !strings.Contains(out, "pinned") {
		t.Fatalf("a pinned module must carry its marker: %q", out)
	}
	for _, box := range []string{"╭", "╮", "╰", "│"} {
		if strings.Contains(out, box) {
			t.Fatalf("screen must be borderless, found %q: %q", box, out)
		}
	}
}

func TestPluginsScreenVersionActionOpensASelectMenu(t *testing.T) {
	f := &fakePluginSeam{rows: testRows(), versions: []string{"v1.0.0", "v1.1.0"}}
	m := openPluginsScreen(t, f)

	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // open the actions for mod/a
	if !strings.Contains(m.pluginsView(), pluginActionVersion) {
		t.Fatalf("actions must offer a version choice: %q", m.pluginsView())
	}
	m.pluginsMenuIdx = indexOf(m.pluginsMenu, pluginActionVersion)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(runCmd(cmd))

	out := m.pluginsView()
	if !strings.Contains(out, "v1.0.0") || !strings.Contains(out, "v1.1.0") {
		t.Fatalf("choosing a version must open the published list: %q", out)
	}
	if len(f.applied) != 0 {
		t.Fatalf("opening a menu must not write: %v", f.applied)
	}
}

func TestPluginsScreenWarningAbortWritesNothing(t *testing.T) {
	f := &fakePluginSeam{rows: testRows(), findings: []string{"mod/a moves back from v1.0.0 to v0.9.0"}}
	m := openPluginsScreen(t, f)

	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // actions
	m.pluginsMenuIdx = indexOf(m.pluginsMenu, pluginActionBump)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(runCmd(cmd)) // the findings arrive

	out := m.pluginsView()
	if !strings.Contains(out, "moves back from v1.0.0") {
		t.Fatalf("the warning must name what was found: %q", out)
	}
	if !strings.Contains(out, pluginWarnAbort) {
		t.Fatalf("the warning must be a select menu: %q", out)
	}
	m.pluginsMenuIdx = indexOf(m.pluginsMenu, pluginWarnAbort)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(runCmd(cmd))

	if len(f.applied) != 0 {
		t.Fatalf("aborting must leave the composition untouched: %v", f.applied)
	}
}

func TestPluginsScreenFailedBuildOffersRestoreOrKeep(t *testing.T) {
	f := &fakePluginSeam{
		rows:     testRows(),
		output:   "plugins.go:12: undefined: contracts.Register",
		applyErr: errors.New("exit status 1"),
	}
	m := openPluginsScreen(t, f)

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.pluginsMenuIdx = indexOf(m.pluginsMenu, pluginActionBump)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(runCmd(cmd)) // findings (none) → the warning menu
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(runCmd(cmd)) // the apply fails

	out := m.pluginsView()
	if !strings.Contains(out, "undefined: contracts.Register") {
		t.Fatalf("the compiler's own output must be shown: %q", out)
	}
	if !strings.Contains(out, pluginFailRestore) || !strings.Contains(out, pluginFailKeep) {
		t.Fatalf("a failed build must offer both outcomes: %q", out)
	}
	m.pluginsMenuIdx = indexOf(m.pluginsMenu, pluginFailRestore)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(runCmd(cmd))
	if f.restored != 1 {
		t.Fatalf("choosing restore must put the tree back, restored=%d", f.restored)
	}
}

func TestPluginsScreenSuccessEndsOnTheRestartLine(t *testing.T) {
	f := &fakePluginSeam{rows: testRows(), output: "go: upgraded mod/a v1.0.0 => v1.1.0"}
	m := openPluginsScreen(t, f)

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.pluginsMenuIdx = indexOf(m.pluginsMenu, pluginActionBump)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(runCmd(cmd))
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(runCmd(cmd))

	out := m.pluginsView()
	if !strings.Contains(out, "next restart") {
		t.Fatalf("a successful apply must say the change waits for a restart: %q", out)
	}
	if len(f.applied) != 1 || !strings.HasPrefix(f.applied[0], string(PluginBump)) {
		t.Fatalf("the seam must have been asked to bump: %v", f.applied)
	}
	if f.restored != 0 {
		t.Fatalf("a successful apply must not restore anything")
	}
}

func TestPluginsScreenEscCloses(t *testing.T) {
	m := openPluginsScreen(t, &fakePluginSeam{rows: testRows()})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pluginsOpen {
		t.Fatal("Esc must close the plugins screen")
	}
}

func TestPluginsScreenNavClamps(t *testing.T) {
	m := openPluginsScreen(t, &fakePluginSeam{rows: testRows()})
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.pluginsIdx != 0 {
		t.Fatalf("up at the top must stay at 0, got %d", m.pluginsIdx)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.pluginsIdx != 1 {
		t.Fatalf("down past the end must clamp to the last row, got %d", m.pluginsIdx)
	}
}

// TestPluginsScreenPaletteEntry checks the screen has a way in from the palette.
func TestPluginsScreenPaletteEntry(t *testing.T) {
	found := false
	for _, c := range localCommands() {
		if c.Name == "plugins" {
			found = true
		}
	}
	if !found {
		t.Fatal("the palette must offer the plugins screen")
	}
}

func indexOf(rows []string, want string) int {
	for i, r := range rows {
		if r == want {
			return i
		}
	}
	return -1
}
