package termpaint_test

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

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
		{name: "RLO bidi override", in: "a‮b", want: `a‮b`},
		{name: "LRI bidi isolate", in: "a⁦b", want: `a⁦b`},
		{name: "LRM and RLM marks", in: "a‎b‏c", want: `a‎b‏c`},
		{name: "ALM arabic letter mark", in: "a؜b", want: `a؜b`},
		{name: "line and paragraph separators", in: "a b c", want: `a b c`},
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

func TestPrintableOutputCarriesNoControlByte(t *testing.T) {
	in := "\x1b]8;;x\x07\u009b\r\n\x00\x7f\xff" + "ok"
	got := termpaint.Printable(in)
	for _, r := range got {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("Printable(%q) = %q still carries control %U", in, got, r)
		}
	}
}
