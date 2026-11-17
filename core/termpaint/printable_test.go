package termpaint_test

import (
	"fmt"
	"slices"
	"testing"
	"unicode"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

func bidiControls() []rune {
	return []rune{
		0x061c, 0x200e, 0x200f, 0x2028, 0x2029,
		0x202a, 0x202b, 0x202c, 0x202d, 0x202e,
		0x2066, 0x2067, 0x2068, 0x2069,
	}
}

func TestPrintableWritesEveryControlCharacterAsVisibleEscape(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "ESC and CSI", in: "\x1b[31mred\x1b[0m", want: `\x1b[31mred\x1b[0m`},
		{name: "OSC ended by BEL", in: "\x1b]0;title\a", want: `\x1b]0;title\a`},
		{name: "OSC 8 ended by ST", in: "\x1b]8;;https://example.test\x1b\\link", want: `\x1b]8;;https://example.test\x1b\link`},
		{name: "OSC 52 clipboard", in: "\x1b]52;c;aGk=\a", want: `\x1b]52;c;aGk=\a`},
		{name: "C1 CSI", in: "a\u009bb", want: `a\u009bb`},
		{name: "CR", in: "ab\rcd", want: `ab\rcd`},
		{name: "LF", in: "one\ntwo", want: `one\ntwo`},
		{name: "TAB", in: "a\tb", want: `a\tb`},
		{name: "DEL", in: "a\x7fb", want: `a\x7fb`},
		{name: "NUL", in: "a\x00b", want: `a\x00b`},
		{name: "invalid UTF-8 byte 0xff", in: "a\xffb", want: `a\xffb`},
		{name: "invalid UTF-8 byte 0xc3 before a non-continuation", in: "a\xc3b", want: `a\xc3b`},
		{name: "invalid UTF-8 continuation byte 0x80", in: "\x80", want: `\x80`},
		{name: "RLO bidi override", in: "a\u202eb", want: `a\u202eb`},
		{name: "LRI bidi isolate", in: "a\u2066b", want: `a\u2066b`},
		{name: "LRM and RLM marks", in: "a\u200eb\u200fc", want: `a\u200eb\u200fc`},
		{name: "ALM arabic letter mark", in: "a\u061cb", want: `a\u061cb`},
		{name: "line and paragraph separators", in: "a\u2028b\u2029c", want: `a\u2028b\u2029c`},
		{name: "ZWJ joins an emoji sequence", in: "👨‍👩‍👧", want: "👨‍👩‍👧"},
		{name: "valid UTF-8 unchanged", in: "démon ✕ ● 日本 🟢", want: "démon ✕ ● 日本 🟢"},
		{name: "replacement character unchanged", in: "a�b", want: "a�b"},
		{name: "empty", in: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := termpaint.Printable(tc.in); got != tc.want {
				t.Errorf("Printable(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPrintableEscapesTheBoundsOfEveryBidiRange(t *testing.T) {
	for _, char := range bidiControls() {
		want := fmt.Sprintf(`\u%04x`, char)
		if got := termpaint.Printable(string(char)); got != want {
			t.Errorf("Printable(U+%04X) = %q, want %q", char, got, want)
		}
	}
}

func TestPrintableLeavesTheNeighboursOfBidiControlsIntact(t *testing.T) {
	neighbours := []rune{
		0x2065,
		0x206a,
		0x2027,
		0x202f,
		0x200d,
		0x200c,
		0x061b,
		0x2030,
	}
	for _, char := range neighbours {
		if got := termpaint.Printable(string(char)); got != string(char) {
			t.Errorf("Printable(U+%04X) = %q, want it unchanged", char, got)
		}
	}
}

func TestPrintableOutputCarriesNoControlByte(t *testing.T) {
	in := "\x1b]8;;x\x07\u009b\r\n\x00\x7f\xff" + "ok"
	for _, char := range bidiControls() {
		in += string(char)
	}
	got := termpaint.Printable(in)
	for _, r := range got {
		if unicode.IsControl(r) || slices.Contains(bidiControls(), r) {
			t.Fatalf("Printable(%q) = %q still carries control %U", in, got, r)
		}
	}
}
