package tui

import (
	"strings"
	"unicode"

	rw "github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// displayRows reports how many visual rows the textarea composer needs to render
// value at content width, mirroring bubbles/textarea's own soft-wrap. The widget
// grows only with explicit newlines (LineCount counts logical lines), so a long
// unbroken line would otherwise stay one row tall and scroll horizontally — the
// jitter the operator sees while typing. Feeding this count into SetHeight lets
// the composer grow with the wrapped draft instead.
//
// width is the textarea content width (m.input.Width(), already net of prompt and
// line numbers); value is its full text (m.input.Value()).
func displayRows(value string, width int) int {
	if width <= 0 {
		// No usable width yet: fall back to logical lines so we never under-report.
		return max(1, strings.Count(value, "\n")+1)
	}
	rows := 0
	for _, line := range strings.Split(value, "\n") {
		rows += len(composerWrap([]rune(line), width))
	}
	return max(1, rows)
}

// composerWrap is a verbatim port of bubbles/textarea's unexported wrap, kept in
// lockstep with the pinned widget version so displayRows matches what the widget
// actually renders row-for-row (including its trailing-space extra row at an exact
// width fill). It word-wraps runes into display lines of at most width columns.
func composerWrap(runes []rune, width int) [][]rune {
	var (
		lines  = [][]rune{{}}
		word   = []rune{}
		row    int
		spaces int
	)

	for _, r := range runes {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word = append(word, r)
		}

		if spaces > 0 {
			if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces > width {
				row++
				lines = append(lines, []rune{})
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], repeatSpaces(spaces)...)
				spaces = 0
				word = nil
			} else {
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], repeatSpaces(spaces)...)
				spaces = 0
				word = nil
			}
		} else {
			lastCharLen := rw.RuneWidth(word[len(word)-1])
			if uniseg.StringWidth(string(word))+lastCharLen > width {
				if len(lines[row]) > 0 {
					row++
					lines = append(lines, []rune{})
				}
				lines[row] = append(lines[row], word...)
				word = nil
			}
		}
	}

	if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces >= width {
		lines = append(lines, []rune{})
		lines[row+1] = append(lines[row+1], word...)
		spaces++
		lines[row+1] = append(lines[row+1], repeatSpaces(spaces)...)
	} else {
		lines[row] = append(lines[row], word...)
		spaces++
		lines[row] = append(lines[row], repeatSpaces(spaces)...)
	}

	return lines
}

func repeatSpaces(n int) []rune {
	return []rune(strings.Repeat(" ", n))
}
