package termpaint_test

import (
	"slices"
	"testing"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

func TestWidthLeavesEscapeSequencesOut(t *testing.T) {
	pal := termpaint.ANSIPalette()
	if got := termpaint.Width(termpaint.Paint(pal.Bad, "✕ panne")); got != len([]rune("✕ panne")) {
		t.Errorf("Width(painted) = %d, want %d", got, len([]rune("✕ panne")))
	}
}

func TestTableAlignsPaintedCellsLikePlainOnes(t *testing.T) {
	pal := termpaint.ANSIPalette()
	plain := &termpaint.Table{Right: map[int]bool{1: true}}
	painted := &termpaint.Table{Right: map[int]bool{1: true}}
	for _, row := range [][2]string{{"démon", "2 ms"}, {"claude · pro", "142 ms"}} {
		plain.Add(row[0], row[1])
		painted.Add(termpaint.Paint(pal.Good, row[0]), row[1])
	}
	widths := func(lines []string) []int {
		out := make([]int, len(lines))
		for index, line := range lines {
			out[index] = termpaint.Width(line)
		}
		return out
	}
	if got, want := widths(painted.Lines()), widths(plain.Lines()); !slices.Equal(got, want) {
		t.Errorf("painted line widths = %v, want %v", got, want)
	}
}

func TestTableSpanTakesNoPartInTheColumnWidths(t *testing.T) {
	table := &termpaint.Table{}
	table.Span("a group title much wider than any cell")
	table.Add("ab", "c")
	if got := table.Lines()[1]; got != "ab  c" {
		t.Errorf("row after a span = %q, want %q", got, "ab  c")
	}
}
