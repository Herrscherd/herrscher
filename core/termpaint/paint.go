// Package termpaint colors and aligns the text reports the CLI and the terminal gateway
// share: a palette of roles, the visible width of painted text, and a table whose columns
// stay aligned whatever the colors.
package termpaint

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	ansiReset = "\x1b[0m"
	columnGap = 2
)

// Style paints a piece of text for one role of a report.
type Style func(string) string

// Palette holds the style of each role of a report. A nil style leaves its text plain,
// so the zero Palette renders plain text.
type Palette struct {
	Accent  Style
	Fill    Style
	Marker  Style
	Dim     Style
	Strong  Style
	Bad     Style
	Warn    Style
	Good    Style
	Group   Style
	Inverse Style
}

// ANSIPalette is the palette of a true color terminal, the colors of the validated mockup.
func ANSIPalette() Palette {
	return Palette{
		Accent:  sgr("38;2;224;137;106"),
		Fill:    sgr("38;2;224;137;106"),
		Marker:  sgr("1;38;2;255;255;255"),
		Dim:     sgr("38;2;107;114;128"),
		Strong:  sgr("1;38;2;255;255;255"),
		Bad:     sgr("38;2;248;113;113"),
		Warn:    sgr("38;2;251;191;36"),
		Good:    sgr("38;2;74;222;128"),
		Group:   sgr("38;2;196;181;253"),
		Inverse: sgr("1;38;2;11;13;18;48;2;230;232;238"),
	}
}

func sgr(code string) Style {
	open := "\x1b[" + code + "m"
	return func(text string) string { return open + text + ansiReset }
}

// Paint renders text in style, or leaves it as it is when the style is nil or the text
// empty.
func Paint(style Style, text string) string {
	if style == nil || text == "" {
		return text
	}
	return style(text)
}

// Width is the number of terminal cells text takes, its escape sequences left out.
func Width(text string) int {
	return ansi.StringWidth(text)
}

// PadRight fills text with spaces up to width cells.
func PadRight(text string, width int) string {
	return text + strings.Repeat(" ", max(width-Width(text), 0))
}

// PadLeft puts spaces before text up to width cells.
func PadLeft(text string, width int) string {
	return strings.Repeat(" ", max(width-Width(text), 0)) + text
}

type tableRow struct {
	cells []string
	span  bool
}

// Table lays rows of painted cells out in columns two cells apart. Right names the
// columns aligned to the right; a span is a line of its own that takes no part in the
// widths of the columns.
type Table struct {
	Right map[int]bool
	rows  []tableRow
}

// Add appends a row of cells.
func (t *Table) Add(cells ...string) {
	t.rows = append(t.rows, tableRow{cells: cells})
}

// Span appends a line written as it is, between the rows.
func (t *Table) Span(line string) {
	t.rows = append(t.rows, tableRow{cells: []string{line}, span: true})
}

// Lines renders the table, one string per row, without trailing spaces.
func (t *Table) Lines() []string {
	widths := t.widths()
	lines := make([]string, 0, len(t.rows))
	for _, row := range t.rows {
		if row.span {
			lines = append(lines, row.cells[0])
			continue
		}
		var line strings.Builder
		for column, cell := range row.cells {
			if column > 0 {
				line.WriteString(strings.Repeat(" ", columnGap))
			}
			line.WriteString(t.aligned(column, cell, widths[column]))
		}
		lines = append(lines, strings.TrimRight(line.String(), " "))
	}
	return lines
}

func (t *Table) widths() []int {
	var widths []int
	for _, row := range t.rows {
		if row.span {
			continue
		}
		for column, cell := range row.cells {
			if column == len(widths) {
				widths = append(widths, 0)
			}
			widths[column] = max(widths[column], Width(cell))
		}
	}
	return widths
}

func (t *Table) aligned(column int, cell string, width int) string {
	if t.Right[column] {
		return PadLeft(cell, width)
	}
	return PadRight(cell, width)
}
