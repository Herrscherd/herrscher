package clitext

import (
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestFormatDurationWritesItsDecimalInEachLanguage(t *testing.T) {
	tests := []struct {
		name string
		lang i18n.Language
		d    time.Duration
		want string
	}{
		{name: "fr seconds take a decimal comma", lang: i18n.French, d: 1500 * time.Millisecond, want: "1,5 s"},
		{name: "en seconds take a decimal point", lang: i18n.English, d: 1500 * time.Millisecond, want: "1.5 s"},
		{name: "fr milliseconds below a second", lang: i18n.French, d: 250 * time.Millisecond, want: "250 ms"},
		{name: "en zero is milliseconds", lang: i18n.English, want: "0 ms"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatDuration(tc.lang, tc.d); got != tc.want {
				t.Fatalf("FormatDuration = %q, want %q", got, tc.want)
			}
		})
	}
}
