package tui

import (
	"strings"
	"unicode"

	rw "github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

func displayRows(value string, width int) int {
	if width <= 0 {
		return max(1, strings.Count(value, "\n")+1)
	}
	rows := 0
	for _, line := range strings.Split(value, "\n") {
		rows += len(composerWrap([]rune(line), width))
	}
	return max(1, rows)
}

type composerWrapper struct {
	lines  [][]rune
	word   []rune
	row    int
	spaces int
	width  int
}

func composerWrap(runes []rune, width int) [][]rune {
	w := &composerWrapper{lines: [][]rune{{}}, word: []rune{}, width: width}
	for _, r := range runes {
		if unicode.IsSpace(r) {
			w.spaces++
		} else {
			w.word = append(w.word, r)
		}
		if w.spaces > 0 {
			w.placeWord()
		} else {
			w.breakLongWord()
		}
	}
	w.placeTrailing()
	return w.lines
}

func (w *composerWrapper) placeWord() {
	if w.lineWidth()+w.wordWidth()+w.spaces > w.width {
		w.newRow()
	}
	w.appendWordAndSpaces(w.spaces)
}

func (w *composerWrapper) breakLongWord() {
	lastCharLen := rw.RuneWidth(w.word[len(w.word)-1])
	if uniseg.StringWidth(string(w.word))+lastCharLen > w.width {
		if len(w.lines[w.row]) > 0 {
			w.newRow()
		}
		w.lines[w.row] = append(w.lines[w.row], w.word...)
		w.word = nil
	}
}

func (w *composerWrapper) placeTrailing() {
	if w.lineWidth()+w.wordWidth()+w.spaces >= w.width {
		w.newRow()
	}
	w.appendWordAndSpaces(w.spaces + 1)
}

func (w *composerWrapper) appendWordAndSpaces(spaces int) {
	w.lines[w.row] = append(w.lines[w.row], w.word...)
	w.lines[w.row] = append(w.lines[w.row], repeatSpaces(spaces)...)
	w.spaces = 0
	w.word = nil
}

func (w *composerWrapper) newRow() {
	w.row++
	w.lines = append(w.lines, []rune{})
}

func (w *composerWrapper) lineWidth() int {
	return uniseg.StringWidth(string(w.lines[w.row]))
}

func (w *composerWrapper) wordWidth() int {
	return uniseg.StringWidth(string(w.word))
}

func repeatSpaces(n int) []rune {
	return []rune(strings.Repeat(" ", n))
}
