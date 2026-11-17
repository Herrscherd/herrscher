package termpaint

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const hexDigits = "0123456789abcdef"

const (
	nibbleShift = 4
	nibbleMask  = 0x0f

	arabicLetterMark       rune = 0x061c
	leftToRightMark        rune = 0x200e
	rightToLeftMark        rune = 0x200f
	lineSeparator          rune = 0x2028
	paragraphSeparator     rune = 0x2029
	firstEmbeddingOverride rune = 0x202a
	lastEmbeddingOverride  rune = 0x202e
	firstIsolate           rune = 0x2066
	lastIsolate            rune = 0x2069
)

// Printed is text that has been through [Escape]. It is the only way to hold text
// that a terminal may show, so a Printed value never carries a raw control to it.
type Printed struct {
	text string
}

// Escape returns text as a terminal shows it without obeying it: every control
// character (C0, DEL and C1) and every bidirectional control (U+202A to U+202E,
// U+2066 to U+2069, U+200E, U+200F, U+061C, U+2028, U+2029) is written as its Go
// escape, so ESC reads \x1b, BEL \a, CR \r, LF \n, U+009B \u009b and RLO \u202e,
// and every byte that is not valid UTF-8 reads \xNN. Valid text comes back unchanged.
func Escape(text string) Printed {
	return Printed{text: escapeControls(text)}
}

// String returns the escaped text, ready to paint.
func (p Printed) String() string {
	return p.text
}

func escapeControls(text string) string {
	var out strings.Builder
	for index := 0; index < len(text); {
		char, size := utf8.DecodeRuneInString(text[index:])
		switch {
		case char == utf8.RuneError && size == 1:
			writeByteEscape(&out, text[index])
		case isEscaped(char):
			out.WriteString(controlEscape(char))
		default:
			out.WriteString(text[index : index+size])
		}
		index += size
	}
	return out.String()
}

func writeByteEscape(out *strings.Builder, value byte) {
	out.WriteString(`\x`)
	out.WriteByte(hexDigits[value>>nibbleShift])
	out.WriteByte(hexDigits[value&nibbleMask])
}

func isEscaped(char rune) bool {
	return unicode.IsControl(char) || isBidiControl(char)
}

func isBidiControl(char rune) bool {
	switch char {
	case arabicLetterMark, leftToRightMark, rightToLeftMark, lineSeparator, paragraphSeparator:
		return true
	}
	return char >= firstEmbeddingOverride && char <= lastEmbeddingOverride || char >= firstIsolate && char <= lastIsolate
}

func controlEscape(char rune) string {
	quoted := strconv.QuoteRune(char)
	return quoted[1 : len(quoted)-1]
}
