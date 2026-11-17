package termpaint

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const hexDigits = "0123456789abcdef"

// Printable returns text as a terminal shows it without obeying it: every control
// character (C0, DEL and C1) is written as its Go escape, so ESC reads \x1b, BEL
// \a, CR \r, LF \n and U+009B \u009b, and every byte that is not valid UTF-8 reads
// \xNN. Valid text comes back unchanged.
func Printable(text string) string {
	var out strings.Builder
	for index := 0; index < len(text); {
		char, size := utf8.DecodeRuneInString(text[index:])
		switch {
		case char == utf8.RuneError && size == 1:
			writeByteEscape(&out, text[index])
		case unicode.IsControl(char):
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
	out.WriteByte(hexDigits[value>>4])
	out.WriteByte(hexDigits[value&0x0f])
}

func controlEscape(char rune) string {
	quoted := strconv.QuoteRune(char)
	return quoted[1 : len(quoted)-1]
}
