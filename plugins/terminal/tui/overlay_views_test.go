package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/skills"
	"github.com/charmbracelet/x/ansi"
)

func assertFloatingList(t *testing.T, name, out string, width int) {
	t.Helper()
	if out == "" {
		t.Fatalf("%s drew nothing", name)
	}
	rows := strings.Split(out, "\n")
	if !strings.Contains(ansi.Strip(rows[0]), glyphCursor) {
		t.Fatalf("%s must mark its selected row: %q", name, rows[0])
	}
	for i, r := range rows {
		if got := ansi.StringWidth(r); got != width {
			t.Fatalf("%s row %d is %d columns wide, want %d: %q", name, i, got, width, r)
		}
	}
}

func TestPaletteUsesTheFloatingList(t *testing.T) {
	m := sizedTestModel(t, 74)
	m.input.SetValue("/")
	assertFloatingList(t, "palette", m.paletteView(), 74)
}

func TestResumeUsesTheFloatingList(t *testing.T) {
	m := sizedTestModel(t, 74)
	m.resumeRows = []contracts.SessionInfo{{Name: "un"}, {Name: "deux"}}
	m.resumeOpen = true
	assertFloatingList(t, "resume", m.resumeView(), 74)
}

func TestSwitchUsesTheFloatingList(t *testing.T) {
	m := sizedTestModel(t, 74)
	m.switchRows = []contracts.SessionInfo{{Name: "un", ChannelID: "c1"}, {Name: "deux", ChannelID: "c2"}}
	m.switchOpen = true
	assertFloatingList(t, "switch", m.switchView(), 74)
}

func TestSkillsUsesTheFloatingList(t *testing.T) {
	m := sizedTestModel(t, 74)
	m.skillsRows = []skills.Skill{{Name: "pdf-fill", Description: "remplir des PDF"}}
	m.skillsOpen = true
	assertFloatingList(t, "skills", m.skillsView(), 74)
}

func TestMentionUsesTheFloatingList(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m := newModel(&fakeBackend{sessions: []contracts.SessionInfo{{Name: "a", ChannelID: "a", Dir: dir}}})
	m.ensureTab("a")
	m = typeInto(t, m, tea.WindowSizeMsg{Width: 74, Height: 24})
	m.input.SetValue("@no")
	assertFloatingList(t, "mention", m.mentionView(), 74)
}
