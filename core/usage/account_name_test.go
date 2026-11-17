package usage

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

func bidiControls() []rune {
	return []rune{
		0x061c, 0x200e, 0x200f, 0x2028, 0x2029,
		0x202a, 0x202b, 0x202c, 0x202d, 0x202e,
		0x2066, 0x2067, 0x2068, 0x2069,
	}
}

func TestLimitCardsNeutraliseTheDefaultProfileName(t *testing.T) {
	snapshot := Snapshot{
		At: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC),
		Limits: []AccountLimits{
			{Vendor: "claude", Account: "work\x1b]0;pwn\a\u202e", Default: true, Status: LimitsFailed},
		},
	}
	lines, err := LimitCards(snapshot, i18n.English, termpaint.Palette{}, UnicodeEdges(), 140)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Join(lines, "\n")
	for _, r := range out {
		if r != '\n' && (unicode.IsControl(r) || slices.Contains(bidiControls(), r)) {
			t.Fatalf("LimitCards() output carries the raw control %U:\n%q", r, out)
		}
	}
	if want := `work\x1b]0;pwn\a\u202e`; !strings.Contains(out, want) {
		t.Errorf("LimitCards() output does not show the profile name as %q:\n%s", want, out)
	}
}

func TestShareLabelsNeutraliseAccountAndModel(t *testing.T) {
	text := &renderer{lang: i18n.English}
	share := Share{Vendor: "claude", Account: "work\u2066\x1b]0;pwn\a", Model: "opus\x1b[2J"}
	account := accountLabel(share, text)
	if want := `work\u2066\x1b]0;pwn\a`; !strings.Contains(account, want) {
		t.Errorf("accountLabel() = %q, want it to contain %q", account, want)
	}
	if want := `opus\x1b[2J`; modelLabel(share, text) != want {
		t.Errorf("modelLabel() = %q, want %q", modelLabel(share, text), want)
	}
	for _, label := range []string{account, modelLabel(share, text)} {
		for _, r := range label {
			if unicode.IsControl(r) || slices.Contains(bidiControls(), r) {
				t.Fatalf("label %q carries the raw control %U", label, r)
			}
		}
	}
}
