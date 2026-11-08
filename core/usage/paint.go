package usage

import (
	"strings"
	"unicode/utf8"
)

const (
	escapeByte    = '\x1b'
	csiOpen       = '['
	csiPrefixLen  = 2
	csiFinalFirst = '@'
	csiFinalLast  = '~'
	ansiReset     = "\x1b[0m"
)

// Style paints a piece of text for one role of the usage display.
type Style func(string) string

// Palette holds the style of each role of the usage display. A nil style leaves its
// text plain, so the zero Palette renders plain text.
type Palette struct {
	Accent Style
	Fill   Style
	Marker Style
	Dim    Style
	Strong Style
	Bad    Style
	Warn   Style
	Good   Style
}

// ANSIPalette is the palette of a true color terminal, the colors of the validated mockup.
func ANSIPalette() Palette {
	return Palette{
		Accent: ansi("38;2;224;137;106"),
		Fill:   ansi("38;2;224;137;106"),
		Marker: ansi("1;38;2;255;255;255"),
		Dim:    ansi("38;2;107;114;128"),
		Strong: ansi("1;38;2;255;255;255"),
		Bad:    ansi("38;2;248;113;113"),
		Warn:   ansi("38;2;251;191;36"),
		Good:   ansi("38;2;74;222;128"),
	}
}

func ansi(code string) Style {
	open := "\x1b[" + code + "m"
	return func(text string) string { return open + text + ansiReset }
}

func paint(style Style, text string) string {
	if style == nil || text == "" {
		return text
	}
	return style(text)
}

func visibleWidth(text string) int {
	width := 0
	for index := 0; index < len(text); {
		if text[index] == escapeByte && index+1 < len(text) && text[index+1] == csiOpen {
			index = skipEscape(text, index+csiPrefixLen)
			continue
		}
		_, size := utf8.DecodeRuneInString(text[index:])
		index += size
		width++
	}
	return width
}

func skipEscape(text string, from int) int {
	for index := from; index < len(text); index++ {
		if text[index] >= csiFinalFirst && text[index] <= csiFinalLast {
			return index + 1
		}
	}
	return len(text)
}

type table struct {
	rows  [][]string
	right map[int]bool
}

func (t *table) add(cells ...string) {
	t.rows = append(t.rows, cells)
}

func (t *table) lines() []string {
	widths := t.widths()
	lines := make([]string, 0, len(t.rows))
	for _, row := range t.rows {
		var line strings.Builder
		for column, cell := range row {
			if column > 0 {
				line.WriteString(strings.Repeat(" ", tabPadding))
			}
			line.WriteString(t.aligned(column, cell, widths[column]))
		}
		lines = append(lines, strings.TrimRight(line.String(), " "))
	}
	return lines
}

func (t *table) widths() []int {
	var widths []int
	for _, row := range t.rows {
		for column, cell := range row {
			if column == len(widths) {
				widths = append(widths, 0)
			}
			widths[column] = max(widths[column], visibleWidth(cell))
		}
	}
	return widths
}

func (t *table) aligned(column int, cell string, width int) string {
	pad := strings.Repeat(" ", width-visibleWidth(cell))
	if t.right[column] {
		return pad + cell
	}
	return cell + pad
}
