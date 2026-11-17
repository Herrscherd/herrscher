package health

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestArgumentTextShowsTheTextOfEachValue(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
		shown bool
	}{
		{"text", "zeta", "zeta", true},
		{"empty text", "", "", false},
		{"unknown kind holding a JSON string", json.RawMessage(`"zeta"`), "zeta", true},
		{"unknown kind holding an empty JSON string", json.RawMessage(`""`), "", false},
		{"unknown kind holding a JSON number", json.RawMessage(`3`), "3", true},
		{"count keeps its number out of the text", 3, "", false},
		{"no value", nil, "", false},
		{"multiline JSON is shown compacted", json.RawMessage("{\"a\":\n  1}"), `{"a":1}`, true},
		{"null is not shown", json.RawMessage("null"), "", false},
		{"text value at the bound is kept whole", strings.Repeat("a", 4096), strings.Repeat("a", 4096), true},
		{"text value beyond the bound is cut in valid UTF-8", strings.Repeat("a", 5000), strings.Repeat("a", 4096) + "...", true},
		{"JSON string beyond the bound is cut before a four-byte rune", json.RawMessage(`"` + strings.Repeat("a", 4094) + "😀" + `"`), strings.Repeat("a", 4094) + "...", true},
		{"JSON string keeps the decoded ESC byte for the escaping of PR 237", json.RawMessage(`"\u001b[31m"`), "\x1b[31m", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, shown := argumentText(tc.value)
			if got != tc.want || shown != tc.shown {
				t.Fatalf("argumentText(%v) = (%q, %t), want (%q, %t)", tc.value, got, shown, tc.want, tc.shown)
			}
		})
	}
}

func TestNoticeKeepsAnErrorTheRendererReturnsForAnUnsupportedLanguage(t *testing.T) {
	r := newRenderer(clitext.NewMessages(), i18n.Language(200))
	r.notice(contracts.Notice{Code: contracts.CodeSettingInvalid, Args: []contracts.Arg{contracts.Text("Setting", "compactAt"), contracts.Text("Value", "x")}})
	if !errors.Is(r.err, i18n.ErrUnsupportedLanguage) {
		t.Fatalf("notice() error = %v, want ErrUnsupportedLanguage kept", r.err)
	}
}

func TestFallsBackToArgumentsOnlyOnAMismatchedNotice(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"no error renders normally", nil, false},
		{"unknown code falls back", fmt.Errorf("code 9: %w", contracts.ErrUnknownCode), true},
		{"arguments that do not match fall back", fmt.Errorf("argument %q of kind 9: %w", "X", contracts.ErrNoticeArgs), true},
		{"missing catalogue key is kept", fmt.Errorf("code %q: %w", "code.1", contracts.ErrMissingMessage), false},
		{"other errors are kept", errors.New("catalogue unreadable"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fallsBackToArguments(tc.err); got != tc.want {
				t.Fatalf("fallsBackToArguments(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}
