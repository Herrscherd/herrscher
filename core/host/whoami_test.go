package host

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/identity"
)

func TestWhoamiReportNamesEveryKeyAndItsSource(t *testing.T) {
	got := whoamiReport(identity.Identity{Name: "Jane Doe", Email: "jane@example.com", GitHub: "janedoe"})
	// The value column is sized to the widest value, so the report is asserted
	// line by line rather than as one golden block: what matters is that each key
	// carries its value and names where it came from.
	for _, want := range []string{
		"name    Jane Doe         (git config user.name)",
		"email   jane@example.com (git config user.email)",
		"github  janedoe          (git config github.user)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("whoamiReport is missing %q:\n%s", want, got)
		}
	}
}

// An unset key is shown as unset rather than omitted: the operator ran this to
// find out what herrscher believes, and "github is not configured" is an answer.
//
// Asserted without the column padding: "—" is one rune of three bytes, and a
// test that counted the spaces %-19s emits would be asserting Go's padding
// rules rather than the verb's behaviour.
func TestWhoamiReportShowsAnUnsetKey(t *testing.T) {
	got := whoamiReport(identity.Identity{Name: "Jane Doe"})
	if !strings.Contains(got, "(git config github.user, unset)") {
		t.Fatalf("whoamiReport does not mark github unset:\n%s", got)
	}
	if !strings.Contains(got, "github  —") {
		t.Fatalf("whoamiReport does not show an em dash for the unset value:\n%s", got)
	}
}

// Nothing configured is a silence, not a failure — the verb says so, and says
// how to fix it, and the caller exits 0.
func TestWhoamiReportOnSilence(t *testing.T) {
	got := whoamiReport(identity.Identity{})
	if !strings.Contains(got, "git has nothing to say about you here") {
		t.Fatalf("whoamiReport does not name the silence:\n%s", got)
	}
	if !strings.Contains(got, "git config --global user.name") {
		t.Fatalf("whoamiReport does not say how to fix the silence:\n%s", got)
	}
}
