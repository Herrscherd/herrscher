package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func settleAll(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, child := range msg {
			settleAll(m, child)
		}
	case nil:
	default:
		m.Update(msg)
	}
}

func openBoardAndSettle(m *model) {
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	settleAll(m, cmd)
}

func TestPanelAppearsOnlyWhenTheBoardIsWideEnough(t *testing.T) {
	f, m := boardModel(t)
	f.replies = map[string]string{"skill list": "alpha-skill\tactive\tprivate\tlast used never\n"}
	openBoardAndSettle(m)
	for _, width := range []int{60, 100, 120, 200} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		view := m.View()
		shown := strings.Contains(view, "Summary")
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

func TestPanelShowsSkillsFromTheHostAndNAForTheRest(t *testing.T) {
	f, m := boardModel(t)
	f.replies = map[string]string{"skill list": "alpha-skill\tactive\tprivate\tlast used never\nbeta-skill\tactive\tapproved\tlast used 2026-01-01\n"}
	openBoardAndSettle(m)
	view := m.View()
	if !strings.Contains(view, "2 · 1 to approve") {
		t.Fatalf("skills row missing from panel: %s", view)
	}
	if line := lineContaining(view, "cron"); !strings.Contains(line, "n/a") {
		t.Fatalf("cron without a host answer must read n/a, line = %q", line)
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
	if want := "1 turn · " + when.Local().Format("15:04"); !strings.Contains(view, want) {
		t.Fatalf("cron row %q missing from panel: %s", want, view)
	}
	if !strings.Contains(view, "3 rules · 1 deny rule") {
		t.Fatalf("policy row missing from panel: %s", view)
	}
}

func TestPanelShowsNAWhenTheHostAnswersNothing(t *testing.T) {
	_, m := boardModel(t)
	openBoardAndSettle(m)
	view := m.View()
	if !strings.Contains(view, "Summary") {
		t.Fatal("the panel must be drawn at this width even without host data")
	}
	if line := lineContaining(view, "policy"); !strings.Contains(line, "n/a") {
		t.Fatalf("policy without a host answer must read n/a, line = %q", line)
	}
}

func lineContaining(view, want string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	return ""
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
