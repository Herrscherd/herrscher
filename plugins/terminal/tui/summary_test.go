package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func settle(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, child := range msg {
			settle(m, child)
		}
	case nil:
	default:
		m.Update(msg)
	}
}

func openBoardAndSettle(m *model) {
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	settle(m, cmd)
}

func TestPanelAppearsOnlyWhenTheBoardIsWideEnough(t *testing.T) {
	f, m := boardModel(t)
	f.replies = map[string]string{"skill list": "alpha-skill\tactive\tprivate\tlast used never\n"}
	openBoardAndSettle(m)
	for _, width := range []int{60, 100, 120, 200} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		view := m.View()
		shown := strings.Contains(view, "RÉSUMÉ")
		if want := width >= panelMinWidth; shown != want {
			t.Fatalf("width %d: panel shown = %t, want %t", width, shown, want)
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("width %d: line overflows: %q", width, line)
			}
		}
	}
}

func TestPanelShowsSkillsOnlyWhenTheHostAnswered(t *testing.T) {
	f, m := boardModel(t)
	f.replies = map[string]string{"skill list": "alpha-skill\tactive\tprivate\tlast used never\nbeta-skill\tactive\tapproved\tlast used 2026-01-01\n"}
	openBoardAndSettle(m)
	view := m.View()
	if !strings.Contains(view, "2 · 1 à approuver") {
		t.Fatalf("skills row missing from panel: %s", view)
	}
	for _, absent := range []string{"cron", "politique", "vendeurs", "passerelles"} {
		if strings.Contains(view, absent) {
			t.Fatalf("%q has no data from the host and must stay hidden", absent)
		}
	}
}

func TestPanelShowsCronAndPolicyFromTheHost(t *testing.T) {
	const next = "2026-06-01T18:00:00Z"
	when, err := time.Parse(time.RFC3339, next)
	if err != nil {
		t.Fatal(err)
	}
	f, m := boardModel(t)
	f.replies = map[string]string{
		"schedule list": "nightly\tterm\tdaily\tlive\tlast never\tnext " + next + "\nweekly\tterm\tweekly\tpaused\tlast never\tnext -\n",
		"approve rule":  "- allow Bash(git status)\n- deny Bash(rm*)\n- ask Bash(git push*)",
	}
	openBoardAndSettle(m)
	view := m.View()
	if want := "1 active · " + when.Local().Format("15:04"); !strings.Contains(view, want) {
		t.Fatalf("cron row %q missing from panel", want)
	}
	if !strings.Contains(view, "3 règles · 1 noire") {
		t.Fatalf("policy row missing from panel: %s", view)
	}
}

func TestPanelIsHiddenWhenTheHostAnswersNothing(t *testing.T) {
	_, m := boardModel(t)
	openBoardAndSettle(m)
	if strings.Contains(m.View(), "RÉSUMÉ") {
		t.Fatal("a panel without any host data must not be drawn")
	}
}

func TestOpeningASessionMarksItSeenWhileItIsShown(t *testing.T) {
	f, m := boardModel(t)
	openBoardAndSettle(m)
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !containsString(f.seen, "beta") {
		t.Fatalf("opening beta must mark it seen, seen = %v", f.seen)
	}
	f.seen = nil
	m.Update(tickMsg{})
	if !containsString(f.seen, "beta") || containsString(f.seen, "alpha") {
		t.Fatalf("the shown session must be marked on each tick and no other, seen = %v", f.seen)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
