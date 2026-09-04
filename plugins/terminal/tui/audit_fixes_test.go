package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/skills"
)

func manySessions(n int) []contracts.SessionInfo {
	rows := make([]contracts.SessionInfo, n)
	for i := range rows {
		rows[i] = contracts.SessionInfo{Name: fmt.Sprintf("s%02d", i), ChannelID: fmt.Sprintf("c%02d", i)}
	}
	return rows
}

func TestPickerViewsDrawTheSelectedRow(t *testing.T) {
	rows := manySessions(20)
	cases := []struct {
		name string
		sel  int
		view func(*model) string
		set  func(*model, int)
	}{
		{"resume first", 0, (*model).resumeView, func(m *model, i int) { m.resumeRows = rows; m.resumeIdx = i }},
		{"resume past the window", 12, (*model).resumeView, func(m *model, i int) { m.resumeRows = rows; m.resumeIdx = i }},
		{"resume last", 19, (*model).resumeView, func(m *model, i int) { m.resumeRows = rows; m.resumeIdx = i }},
		{"switch past the window", 15, (*model).switchView, func(m *model, i int) { m.switchRows = rows; m.switchIdx = i }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(&fakeBackend{})
			tc.set(m, tc.sel)
			out := tc.view(m)
			if !strings.Contains(out, glyphCursor) {
				t.Fatalf("no cursor drawn:\n%s", out)
			}
			if !strings.Contains(out, rows[tc.sel].Name) {
				t.Fatalf("selected row %q not drawn:\n%s", rows[tc.sel].Name, out)
			}
		})
	}
}

func TestSkillsViewDrawsTheSelectedRow(t *testing.T) {
	m := newModel(&fakeBackend{})
	for i := 0; i < 24; i++ {
		m.skillsRows = append(m.skillsRows, skills.Skill{Name: fmt.Sprintf("skill-%02d", i)})
	}
	m.skillsIdx = 20
	out := m.skillsView()
	if !strings.Contains(out, glyphCursor) || !strings.Contains(out, "skill-20") {
		t.Fatalf("selection not drawn:\n%s", out)
	}
}

func TestApplyImageIgnoresARecycledIndex(t *testing.T) {
	m := newModel(&fakeBackend{})
	tb := &tab{channel: "c1"}
	m.tabs["c1"] = tb
	tb.appendEntry(entry{role: roleAgent, text: "first"})
	stale := imageReadyMsg{channel: "c1", entry: 0, entryID: tb.entries[0].id, escape: "ESC"}

	tb.entries = nil
	tb.appendEntry(entry{role: roleAgent, text: "after a clear"})
	m.applyImage(stale)
	if tb.entries[0].preview != "" {
		t.Fatalf("image grafted onto an unrelated entry: %q", tb.entries[0].preview)
	}

	fresh := imageReadyMsg{channel: "c1", entry: 0, entryID: tb.entries[0].id, escape: "ESC"}
	m.applyImage(fresh)
	if tb.entries[0].preview != "ESC" {
		t.Fatalf("preview = %q, want ESC", tb.entries[0].preview)
	}
}

func TestCheckURLTarget(t *testing.T) {
	cases := []struct {
		target string
		ok     bool
	}{
		{"https://example.com/x", true},
		{"http://example.com", true},
		{"mailto:someone@example.com", true},
		{"file:///home/you/.ssh/id_ed25519", false},
		{"smb://host/share", false},
		{"ms-msdt:/id PCWDiagnostic", false},
		{"example.com/docs", false},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			err := checkURLTarget(tc.target)
			if tc.ok && err != nil {
				t.Fatalf("checkURLTarget(%q) = %v, want nil", tc.target, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("checkURLTarget(%q) = nil, want a refusal", tc.target)
			}
		})
	}
}

func TestMentionCursorFollowsTheCaret(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.input.SetWidth(80)
	m.input.SetValue("@src/ma\nsecond line")
	if got, want := m.mentionCursor(), len("@src/ma\nsecond line"); got != want {
		t.Fatalf("end-of-buffer cursor = %d, want %d", got, want)
	}
	m.input.CursorUp()
	m.input.CursorEnd()
	if got, want := m.mentionCursor(), len("@src/ma"); got != want {
		t.Fatalf("cursor on line 1 = %d, want %d", got, want)
	}
	m.input.CursorStart()
	if got := m.mentionCursor(); got != 0 {
		t.Fatalf("cursor at start = %d, want 0", got)
	}
}

func TestSearchQueryIgnoresModifiedKeys(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		want string
	}{
		{"plain rune", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}, "y"},
		{"alt rune", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}, Alt: true}, ""},
		{"paste", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi"), Paste: true}, "hi"},
		{"space", tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}, " "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(&fakeBackend{})
			m.openSearch()
			m.Update(tc.msg)
			if m.searchQuery != tc.want {
				t.Fatalf("searchQuery = %q, want %q", m.searchQuery, tc.want)
			}
		})
	}
}
