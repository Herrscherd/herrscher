package usage

import (
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

func TestLimitCardsNeutraliseTheDefaultProfileName(t *testing.T) {
	snapshot := Snapshot{
		At: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC),
		Limits: []AccountLimits{
			{Vendor: "claude", Account: "work\x1b]0;pwn\a", Default: true, Status: LimitsFailed},
		},
	}
	lines, err := LimitCards(snapshot, i18n.English, termpaint.Palette{}, UnicodeEdges(), 140)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Join(lines, "\n")
	for _, r := range out {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("LimitCards() output carries the raw control %U:\n%q", r, out)
		}
	}
	if want := `work\x1b]0;pwn\a`; !strings.Contains(out, want) {
		t.Errorf("LimitCards() output does not show the profile name as %q:\n%s", want, out)
	}
}
