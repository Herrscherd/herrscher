package host

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Herrscherd/herrscher/core/identity"
)

func TestWhoamiReportNamesEveryKeyAndItsSource(t *testing.T) {
	got := whoamiReport(identity.Identity{Name: "Jane Doe", Email: "jane@example.com", GitHub: "janedoe"})
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

func TestWhoamiReportShowsAnUnsetKey(t *testing.T) {
	got := whoamiReport(identity.Identity{Name: "Jane Doe"})
	if !strings.Contains(got, "(git config github.user, unset)") {
		t.Fatalf("whoamiReport does not mark github unset:\n%s", got)
	}
	if !strings.Contains(got, "github  -") {
		t.Fatalf("whoamiReport does not show a hyphen for the unset value:\n%s", got)
	}
}

func TestWhoamiReportOnSilence(t *testing.T) {
	got := whoamiReport(identity.Identity{})
	if !strings.Contains(got, "git has nothing to say about you here") {
		t.Fatalf("whoamiReport does not name the silence:\n%s", got)
	}
	if !strings.Contains(got, "git config --global user.name") {
		t.Fatalf("whoamiReport does not say how to fix the silence:\n%s", got)
	}
}

func TestWhoamiReportAlignsAnAccentedName(t *testing.T) {
	got := whoamiReport(identity.Identity{Name: "Léo Sauvage", Email: "leo@example.com", GitHub: "leo"})
	var col int
	for i, line := range strings.Split(got, "\n") {
		at := strings.Index(line, "(git config ")
		if at < 0 {
			t.Fatalf("line %d has no source annotation: %q", i, line)
		}
		at = utf8.RuneCountInString(line[:at])
		if i == 0 {
			col = at
			continue
		}
		if at != col {
			t.Fatalf("annotation starts at rune %d on line %d, %d on the first:\n%s", at, i, col, got)
		}
	}
}
