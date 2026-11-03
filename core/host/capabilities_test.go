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
	want := "  commands\n  gateway: channel read\n  session: close, list"
	if got != want {
		t.Fatalf("summary:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestCapabilitySummaryHasNoDanglingSeparator(t *testing.T) {
	got := CapabilitySummary([]cli.Spec{
		{Path: []string{"update"}},
		{Path: []string{"plugin"}},
		{Path: []string{"plugin", "list"}},
	})
	want := "  plugin: list\n  update"
	if got != want {
		t.Fatalf("summary:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

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
	if got := CapabilityEnvPair(nil, nil); got != "" {
		t.Fatalf("no commands must produce no env pair, got %q", got)
	}
	pair := CapabilityEnvPair([]cli.Spec{{Path: []string{"session", "list"}}}, nil)
	if !strings.HasPrefix(pair, EnvCapabilities+"=") || !strings.Contains(pair, "session: list") {
		t.Fatalf("env pair: %q", pair)
	}
}

func TestTheCapabilityBlockOnlyPromisesWhatASessionMayRun(t *testing.T) {
	pair := CapabilityEnvPair([]cli.Spec{
		{Path: []string{"session", "send"}},
		{Path: []string{"session", "close"}},
		{Path: []string{"approve", "ask"}},
		{Path: []string{"approve", "rule"}},
		{Path: []string{"role", "grant"}},
		{Path: []string{"chat", "channel", "read"}},
	}, func(family string) bool { return family == "chat" })
	for _, promised := range []string{"send", "ask", "channel read"} {
		if !strings.Contains(pair, promised) {
			t.Fatalf("block must promise %q: %s", promised, pair)
		}
	}
	for _, refused := range []string{"close", "rule", "grant"} {
		if strings.Contains(pair, refused) {
			t.Fatalf("block promises %q, which a session may not run: %s", refused, pair)
		}
	}
}
