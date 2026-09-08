package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestFloatingListMarksTheCursorRow(t *testing.T) {
	rows := []overlayRow{
		{label: "/resume", detail: "reprendre une session"},
		{label: "/review", detail: "relire le diff"},
	}
	out := floatingList(rows, 0, "↑↓ naviguer", 74)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if !strings.Contains(lines[0], glyphCursor) {
		t.Fatalf("cursor row missing chevron: %q", lines[0])
	}
	if strings.Contains(lines[1], glyphCursor) {
		t.Fatalf("non-cursor row has a chevron: %q", lines[1])
	}
}

func TestFloatingListRowsAreFullWidth(t *testing.T) {
	rows := []overlayRow{{label: "/resume", detail: "reprendre"}}
	for _, line := range strings.Split(floatingList(rows, 0, "", 74), "\n") {
		if lipgloss.Width(line) != 74 {
			t.Fatalf("line %q is %d columns, want 74", line, lipgloss.Width(line))
		}
	}
}

func TestFloatingListTruncatesLongDetail(t *testing.T) {
	rows := []overlayRow{{label: "/resume", detail: strings.Repeat("x", 200)}}
	for _, line := range strings.Split(floatingList(rows, 0, "", 40), "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("line is %d columns, want <= 40", lipgloss.Width(line))
		}
	}
}

func TestFloatingListWithoutRowsIsEmpty(t *testing.T) {
	if got := floatingList(nil, 0, "footer", 74); got != "" {
		t.Fatalf("empty list rendered %q", got)
	}
}
