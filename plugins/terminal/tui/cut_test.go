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

func TestToastStaysVisibleAtTwentyRows(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m.toast = toast{glyph: m.st.glyphs.attention, title: "Approbation attend", signal: "OSC 9 envoyé", at: time.Now()}
	for range 40 {
		m.toast.body = append(m.toast.body, "une ligne du corps du toast qui déborde la place")
	}
	rows := m.vp.Height
	lines := strings.Split(m.toastView(rows), "\n")
	if len(lines) != max(rows, 2) {
		t.Fatalf("the toast must fill the rows it is given: want %d lines, got %d", max(rows, 2), len(lines))
	}
	if !strings.Contains(lines[0], "Approbation attend") {
		t.Errorf("the toast must keep its title border, got %q", lines[0])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "╰") {
		t.Errorf("the toast must close with its bottom border, got %q", lines[len(lines)-1])
	}
	if rows > 2 && !strings.Contains(lines[len(lines)-2], ellipsis) {
		t.Errorf("the last body row of a squeezed toast must end with %q, got %q", ellipsis, lines[len(lines)-2])
	}
	blank := strings.Repeat(strings.Repeat(" ", 60)+"\n", 19) + strings.Repeat(" ", 60)
	if !strings.Contains(m.overlayToast(blank), "Approbation attend") {
		t.Errorf("the toast must be drawn over the screen at 60x20")
	}
}
