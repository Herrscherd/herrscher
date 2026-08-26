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

// A verb that is a whole family on its own contributes its name and nothing
// else, even where a longer verb shares that first word: " — " with nothing
// after it, or with a stray comma in front, reads as a truncated line.
func TestCapabilitySummaryHasNoDanglingSeparator(t *testing.T) {
	got := CapabilitySummary([]cli.Spec{
		{Path: []string{"update"}},
		{Path: []string{"plugin"}},
		{Path: []string{"plugin", "list"}},
	})
	want := "  plugin — list\n  update"
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
	if got := CapabilityEnvPair(nil, nil); got != "" {
		t.Fatalf("no commands must produce no env pair, got %q", got)
	}
	pair := CapabilityEnvPair([]cli.Spec{{Path: []string{"session", "list"}}}, nil)
	if !strings.HasPrefix(pair, EnvCapabilities+"=") || !strings.Contains(pair, "session — list") {
		t.Fatalf("env pair: %q", pair)
	}
}

// The block is read by a session, and a session is refused most of the daemon's
// registry. Advertising the rest of it is not a harmless surplus: a model told
// it can run `approve rule`, then refused, reads the refusal as a fault to work
// around rather than as the answer.
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
