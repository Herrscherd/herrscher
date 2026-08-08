package bridge

import (
	"strings"
	"testing"
)

func TestWithCapabilitiesNamesTheDaemonAndItsVerbs(t *testing.T) {
	got := withCapabilities("MEM", "  gateway — channel post, channel read\n  session — close, list")
	for _, want := range []string{"MEM", "<capabilities>", "herrscher <verb>", "herrscher commands", "channel read", "session — close, list", "</capabilities>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("capabilities block missing %q in:\n%s", want, got)
		}
	}
}

func TestWithCapabilitiesEmptySummaryUnchanged(t *testing.T) {
	for _, summary := range []string{"", "   \n\t"} {
		if got := withCapabilities("MEM", summary); got != "MEM" {
			t.Fatalf("summary %q must return base unchanged, got %q", summary, got)
		}
	}
}
