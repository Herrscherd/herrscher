package usage

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/termpaint"
	"github.com/Herrscherd/herrscher/core/termpaint/termpainttest"
)

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
		if r != '\n' && (unicode.IsControl(r) || slices.Contains(termpainttest.BidiControls(), r)) {
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
	account := accountLabel(share, text).String()
	model := modelLabel(share, text).String()
	if want := `work\u2066\x1b]0;pwn\a`; !strings.Contains(account, want) {
		t.Errorf("accountLabel() = %q, want it to contain %q", account, want)
	}
	if want := `opus\x1b[2J`; model != want {
		t.Errorf("modelLabel() = %q, want %q", model, want)
	}
	for _, label := range []string{account, model} {
		for _, r := range label {
			if unicode.IsControl(r) || slices.Contains(termpainttest.BidiControls(), r) {
				t.Fatalf("label %q carries the raw control %U", label, r)
			}
		}
	}
}
