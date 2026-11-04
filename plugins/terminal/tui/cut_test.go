package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestCutTextMarksTheCut(t *testing.T) {
	got := cutText("la raison repart à l'agent", 10)
	if ansi.StringWidth(got) != 10 || !strings.HasSuffix(got, ellipsis) {
		t.Errorf("cutText must end with %q at the width, got %q", ellipsis, got)
	}
}

func TestCutTextKeepsWhatFits(t *testing.T) {
	if got := cutText("herrs", 10); got != "herrs" {
		t.Errorf("a text that fits must stay whole, got %q", got)
	}
}

func TestCutTextEmptyWhenNoRoom(t *testing.T) {
	if got := cutText("herrscher", 0); got != "" {
		t.Errorf("no room must render nothing, got %q", got)
	}
}

func TestFitSegmentsDropsWholeSegments(t *testing.T) {
	segments := []string{"↑↓ choisir", "⏎ ouvrir", "lettres filtrer", "échap vider"}
	got := fitSegments(segments, 22)
	if strings.Contains(got, "échap") || strings.Contains(got, "filtrer ·") {
		t.Errorf("the rightmost segments must go first, got %q", got)
	}
	if !strings.HasPrefix(got, "↑↓ choisir · ⏎ ouvrir") {
		t.Errorf("the leftmost segments must stay, got %q", got)
	}
	if lipgloss.Width(got) > 22 {
		t.Errorf("the result must fit the width, got %d columns", lipgloss.Width(got))
	}
}

func TestFitSegmentsKeepsTheFirstSegmentCut(t *testing.T) {
	got := fitSegments([]string{"revenir à ce message", "fermer"}, 8)
	if lipgloss.Width(got) > 8 || !strings.HasSuffix(got, ellipsis) {
		t.Errorf("a lone segment too wide must be cut with %q, got %q", ellipsis, got)
	}
}

func TestToastWidthFillsNarrowWindows(t *testing.T) {
	m := newTestModel()
	m.width = 60
	if got := m.toastBoxWidth(); got != 60-toastFrameCols {
		t.Errorf("below %d columns the toast must span the window, got %d", toastBandBelow, got)
	}
}

func TestToastWidthCapsWideWindows(t *testing.T) {
	m := newTestModel()
	m.width = 200
	if got := m.toastBoxWidth(); got != toastWidth {
		t.Errorf("wide windows must cap the toast at %d columns, got %d", toastWidth, got)
	}
}

func TestBoardFooterFitsItsWidth(t *testing.T) {
	m := newTestModel()
	for _, width := range []int{40, 60, 80, 120} {
		if got := m.boardFooter(width); lipgloss.Width(got) > width {
			t.Errorf("board footer at %d columns is %d wide", width, lipgloss.Width(got))
		}
	}
}

func TestApprovalKeepsEveryChoiceAtTwentyRows(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c1"}, Event: approvalMenuEvent(t, "go test ./...", allApprovalRoles()...)})
	lines := m.approvalLines(time.Now(), 7)
	if len(lines) > 7 {
		t.Fatalf("the approval must fit the rows it is given, got %d lines", len(lines))
	}
	joined := strings.Join(lines, "\n")
	for _, label := range []string{"Une fois", "Pour la session", "Toujours", "Refuser avec une raison"} {
		if !strings.Contains(joined, label) {
			t.Errorf("the approval must keep the choice %q visible at 60x20", label)
		}
	}
}
