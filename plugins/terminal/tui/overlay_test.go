package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Herrscherd/herrscher/core/skills"
)

func TestFloatingListMarksTheCursorRow(t *testing.T) {
	rows := []overlayRow{
		{label: "/resume", detail: "reprendre une session"},
		{label: "/review", detail: "relire le diff"},
	}
	out := floatingList(rows, 0, "↑↓ naviguer", 74, testStyles())
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if !strings.Contains(lines[0], unicodeGlyphs().cursor) {
		t.Fatalf("cursor row missing chevron: %q", lines[0])
	}
	if strings.Contains(lines[1], unicodeGlyphs().cursor) {
		t.Fatalf("non-cursor row has a chevron: %q", lines[1])
	}
}

func TestFloatingListAlignsDetailsPastTheLongestLabel(t *testing.T) {
	rows := []overlayRow{
		{label: "/usage", detail: "coût et limites"},
		{label: "/agents-very-long-name", detail: "agents"},
	}
	lines := strings.Split(floatingList(rows, 0, "", 90, testStyles()), "\n")
	first := strings.Index(lines[0], "coût")
	second := strings.Index(lines[1], "agents ")
	if first < 0 || lipgloss.Width(lines[0][:first]) != lipgloss.Width(lines[1][:second]) {
		t.Fatalf("details are not aligned:\n%q\n%q", lines[0], lines[1])
	}
	if lipgloss.Width(lines[1][:second]) <= lipgloss.Width("    /agents-very-long-name") {
		t.Fatalf("detail overlaps the longest label: %q", lines[1])
	}
}

func TestFloatingListRowsAreFullWidth(t *testing.T) {
	rows := []overlayRow{{label: "/resume", detail: "reprendre"}}
	for _, line := range strings.Split(floatingList(rows, 0, "", 74, testStyles()), "\n") {
		if lipgloss.Width(line) != 74 {
			t.Fatalf("line %q is %d columns, want 74", line, lipgloss.Width(line))
		}
	}
}

func TestFloatingListTruncatesLongDetail(t *testing.T) {
	rows := []overlayRow{{label: "/resume", detail: strings.Repeat("x", 200)}}
	for _, line := range strings.Split(floatingList(rows, 0, "", 40, testStyles()), "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("line is %d columns, want <= 40", lipgloss.Width(line))
		}
	}
}

func TestFloatingListWithoutRowsIsEmpty(t *testing.T) {
	if got := floatingList(nil, 0, "footer", 74, testStyles()); got != "" {
		t.Fatalf("empty list rendered %q", got)
	}
}

func TestOverlayRowsFollowTheWindow(t *testing.T) {
	for _, tc := range []struct{ height, want int }{
		{height: 0, want: overlayRowsMin},
		{height: 9, want: overlayRowsMin},
		{height: 24, want: 8},
		{height: 40, want: overlayRowsMax},
		{height: 120, want: overlayRowsMax},
	} {
		m := newTestModel()
		m.height = tc.height
		if got := m.overlayRows(); got != tc.want {
			t.Fatalf("height %d: overlayRows = %d, want %d", tc.height, got, tc.want)
		}
	}
}

func TestPaletteWalksTheWholeListFromTheKeyboard(t *testing.T) {
	m := sizedTestModel(t, 100)
	m.cmds = nil
	for i := range 35 {
		m.cmds = append(m.cmds, CommandSpec{Name: fmt.Sprintf("verb%02d", i)})
	}
	m.input.SetValue("/")

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.palIdx != 1 {
		t.Fatalf("down: palIdx = %d, want 1", m.palIdx)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if want := 1 + m.overlayRows(); m.palIdx != want {
		t.Fatalf("page down: palIdx = %d, want %d", m.palIdx, want)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if m.palIdx != 34 {
		t.Fatalf("end: palIdx = %d, want the last row", m.palIdx)
	}
	if out := m.paletteView(); !strings.Contains(out, unicodeGlyphs().cursor+" /verb34") {
		t.Fatalf("the last verb must be reachable and drawn: %q", out)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if m.palIdx != 0 {
		t.Fatalf("home: palIdx = %d, want 0", m.palIdx)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.palIdx != 0 {
		t.Fatalf("up on the first row must stay put, got %d", m.palIdx)
	}
}

func TestSkillsPanelWalksFromTheKeyboard(t *testing.T) {
	m := sizedTestModel(t, 100)
	m.skillsOpen = true
	for i := range 14 {
		m.skillsRows = append(m.skillsRows, skills.Skill{Name: fmt.Sprintf("skill%02d", i)})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.skillsIdx != 1 {
		t.Fatalf("down: skillsIdx = %d, want 1", m.skillsIdx)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if m.skillsIdx != 13 {
		t.Fatalf("end: skillsIdx = %d, want the last row", m.skillsIdx)
	}
	if out := m.skillsView(); !strings.Contains(out, "skill13") {
		t.Fatalf("the last skill must be drawn once selected: %q", out)
	}
	if !m.skillsOpen {
		t.Fatalf("navigating must not close the panel")
	}
}
