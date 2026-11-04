package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestFrameHeightCountsBothSides(t *testing.T) {
	f := frame{above: []string{"rail"}, below: []string{"a", "b", "c"}}
	if got := f.height(); got != 4 {
		t.Fatalf("height = %d, want 4", got)
	}
}

func TestFrameRenderOrdersZonesAroundViewport(t *testing.T) {
	f := frame{above: []string{"rail"}, below: []string{"box", "status"}}
	want := "rail\nbody\nbox\nstatus"
	if got := f.render("body"); got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

func TestFrameRenderKeepsMultiLineViewportIntact(t *testing.T) {
	f := frame{above: []string{"rail"}, below: []string{"status"}}
	want := "rail\none\ntwo\nstatus"
	if got := f.render("one\ntwo"); got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

func TestFitBlockKeepsHeadAndFooter(t *testing.T) {
	lines := []string{"top", "a", "b", "c", "bottom"}
	if got := fitBlock(lines, 3); strings.Join(got, "|") != "top|a|bottom" {
		t.Fatalf("fitBlock = %q", got)
	}
	if got := fitBlock(lines, 0); got != nil {
		t.Fatalf("fitBlock without room = %q, want nothing", got)
	}
	if got := fitBlock(lines, 9); len(got) != len(lines) {
		t.Fatalf("fitBlock with room = %q, want every line", got)
	}
}

func surfaceOpeners() map[string]func(t *testing.T, m *model) {
	return map[string]func(t *testing.T, m *model){
		"approbation": func(t *testing.T, m *model) {
			t.Helper()
			m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c1"}, Event: approvalMenuEvent(t, "go test ./plugins/terminal/tui/", allApprovalRoles()...)})
		},
		"modes":    func(_ *testing.T, m *model) { m.openPlanMenu("c1") },
		"contexte": func(_ *testing.T, m *model) { m.openContext() },
		"barre":    func(_ *testing.T, m *model) { m.openStatusline() },
		"retour": func(_ *testing.T, m *model) {
			m.rwRows = []rewindRow{{Index: 1, At: "14:02", Text: "ajoute une file de messages"}, {Index: 2, At: "14:19", Text: "relance les tests"}}
			m.rwIdx = 1
			m.rwOpt = rewindOptionBoth
			m.rwStep = rewindStepList
			m.rwOpen = true
			m.relayout()
		},
	}
}

func TestSurfacesFitTheScreenAtEverySize(t *testing.T) {
	for name, open := range surfaceOpeners() {
		for _, size := range []struct{ width, height int }{{60, 20}, {60, 30}, {80, 30}, {120, 30}, {200, 30}} {
			t.Run(fmt.Sprintf("%s %dx%d", name, size.width, size.height), func(t *testing.T) {
				m, _ := barModelAt(t, "")
				m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
				open(t, m)
				assertFitsScreen(t, m.View(), size.width, size.height)
			})
		}
	}
}

func assertFitsScreen(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		t.Errorf("view has %d lines, the screen has %d", len(lines), height)
	}
	for index, line := range lines {
		if lipgloss.Width(line) > width {
			t.Errorf("line %d is %d cells wide, the screen is %d", index, lipgloss.Width(line), width)
		}
	}
}
