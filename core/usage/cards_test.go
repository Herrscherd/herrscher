package usage

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

func TestLimitCardsKeepTheBoxClosed(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	for _, width := range []int{60, 100, 140} {
		for _, pal := range []termpaint.Palette{{}, termpaint.ANSIPalette()} {
			lines, err := LimitCards(limitsSnapshot(now), i18n.French, pal, UnicodeEdges(), width)
			if err != nil {
				t.Fatal(err)
			}
			if widths := boxWidths(lines); len(widths) != 1 || widths[0] > width {
				t.Errorf("width %d: box lines have widths %v, want one width within the panel", width, widths)
			}
		}
	}
}

func boxWidths(lines []string) []int {
	var widths []int
	for _, line := range lines {
		if width := termpaint.Width(line); strings.ContainsAny(line, "┌│├└") && !slices.Contains(widths, width) {
			widths = append(widths, width)
		}
	}
	return widths
}

func TestLimitCardsGroupAccountsByWindow(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	lines, err := LimitCards(limitsSnapshot(now), i18n.French, termpaint.Palette{}, UnicodeEdges(), 140)
	if err != nil {
		t.Fatal(err)
	}
	report := strings.Join(lines, "\n")
	for _, want := range []string{"✳ Claude", "Hebdo", "51 % restant", "▐ pro ★", "5 h", "▐ perso", "Hebdo · Fable", "◎ Codex  natif · non disponible"} {
		if !strings.Contains(report, want) {
			t.Errorf("cards miss %q:\n%s", want, report)
		}
	}
}
