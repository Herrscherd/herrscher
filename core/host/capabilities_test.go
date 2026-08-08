package host

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
)

func TestCapabilitySummaryGroupsByFamily(t *testing.T) {
	got := CapabilitySummary([]cli.Spec{
		{Path: []string{"session", "list"}},
		{Path: []string{"gateway", "channel", "read"}},
		{Path: []string{"session", "close"}},
		{Path: []string{"commands"}},
	})
	want := "  commands\n  gateway — channel read\n  session — close, list"
	if got != want {
		t.Fatalf("summary:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// The block rides in every prompt, so its text must be a function of the build
// alone: a summary that reordered itself between two daemon starts would spend
// the prompt cache of every session for nothing.
func TestCapabilitySummaryIsStable(t *testing.T) {
	specs := []cli.Spec{
		{Path: []string{"memory", "search"}},
		{Path: []string{"agent", "list"}},
		{Path: []string{"memory", "record"}},
	}
	first := CapabilitySummary(specs)
	for range 5 {
		if got := CapabilitySummary(specs); got != first {
			t.Fatalf("summary must not vary between calls:\n%s\n%s", first, got)
		}
	}
}

func TestCapabilityEnvPairEmptyWithoutCommands(t *testing.T) {
	if got := CapabilityEnvPair(nil); got != "" {
		t.Fatalf("no commands must produce no env pair, got %q", got)
	}
	pair := CapabilityEnvPair([]cli.Spec{{Path: []string{"session", "list"}}})
	if !strings.HasPrefix(pair, EnvCapabilities+"=") || !strings.Contains(pair, "session — list") {
		t.Fatalf("env pair: %q", pair)
	}
}
