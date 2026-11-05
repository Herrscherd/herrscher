package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestKillAgentsInterruptsOnlyBusySessions(t *testing.T) {
	f := &fakeBackend{sessions: []contracts.SessionInfo{{ChannelID: "a", Name: "s1"}, {ChannelID: "b", Name: "s2"}}}
	m := newModel(f)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.ensureTab("a")
	m.ensureTab("b")
	m.active = "a"
	m.tabs["a"].busy = true
	pressChord(m, "ctrl+x ctrl+k")
	if len(f.interrupted) != 1 || f.interrupted[0] != "s1" {
		t.Fatalf("killAgents must interrupt only the busy session: %+v", f.interrupted)
	}
}

func TestChatUndoRestoresThePreviousComposerText(t *testing.T) {
	m, _ := queueFixture(t)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	pressChord(m, "ctrl+_")
	if got := m.input.Value(); got != "a" {
		t.Fatalf("undo must restore the previous composer text, got %q", got)
	}
	pressChord(m, "ctrl+_")
	if got := m.input.Value(); got != "" {
		t.Fatalf("a second undo must restore the empty composer, got %q", got)
	}
}

func TestCtrlOTogglesToolDetail(t *testing.T) {
	m, _ := queueFixture(t)
	before := m.expandTools
	pressChord(m, "ctrl+o")
	if m.expandTools == before {
		t.Fatal("ctrl+o must toggle the tool detail mode")
	}
	pressChord(m, "ctrl+o")
	if m.expandTools != before {
		t.Fatal("a second ctrl+o must restore the tool detail mode")
	}
}

func TestChangedBindingShowsInBusyFooter(t *testing.T) {
	m, _ := queueFixture(t)
	m.bindings = loadForTest(t, `{"bindings":[{"context":"Chat","bindings":{"alt+a":"chat:queueRedirect"}}]}`, "", keysSection{}).table
	m.tabs["a"].busy = true
	if view := ansi.Strip(m.View()); !strings.Contains(view, "⌥A redirect") {
		t.Fatalf("the busy footer must show the rebound chord: %s", view)
	}
}

func TestFootersFitEverySizeWithTwentyLines(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m, _ := queueFixture(t)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
		m.tabs["a"].busy = true
		lines := strings.Split(m.View(), "\n")
		if len(lines) != 20 {
			t.Fatalf("width %d: rendered %d lines, want 20", width, len(lines))
		}
		for index, line := range lines {
			if w := ansi.StringWidth(line); w > width {
				t.Fatalf("width %d: line %d is %d columns: %q", width, index, w, ansi.Strip(line))
			}
		}
	}
}
