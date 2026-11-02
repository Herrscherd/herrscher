package tui

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestSeedScrollbackReplaysRealRoles(t *testing.T) {
	fb := &fakeBackend{
		fe:       make(chan RoutedEvent, 1),
		sessions: []contracts.SessionInfo{{Name: "s", ChannelID: "a"}},
		scrollback: map[string][]contracts.ScrollbackLine{"s": {
			{Role: "user", Text: "hello"},
			{Role: "assistant", Text: "hi there"},
		}},
	}
	m := newModel(fb)
	tb := m.ensureTab("a")

	var roles []string
	for _, e := range tb.entries {
		roles = append(roles, e.role)
	}
	want := []string{roleYou, roleAgent, roleScrollback}
	if len(roles) != len(want) {
		t.Fatalf("replayed roles = %v, want %v", roles, want)
	}
	for i, r := range want {
		if roles[i] != r {
			t.Fatalf("entry %d role = %q, want %q (all: %v)", i, roles[i], r, roles)
		}
	}
	if got := tb.entries[0].text; got != "hello" {
		t.Errorf("replayed text must not be prefixed by hand: %q", got)
	}
}

func TestSeedScrollbackAddsNoBoundaryWithoutHistory(t *testing.T) {
	fb := &fakeBackend{
		fe:       make(chan RoutedEvent, 1),
		sessions: []contracts.SessionInfo{{Name: "s", ChannelID: "a"}},
	}
	if tb := newModel(fb).ensureTab("a"); len(tb.entries) != 0 {
		t.Fatalf("empty history seeded %d entries", len(tb.entries))
	}
}

func TestUsageReportNamesTheNumbers(t *testing.T) {
	m := newTestModel()
	tb := m.ensureTab("a")
	m.active = "a"
	tb.ctxTokens = 50_000
	tb.costTotal = 1.25
	out := m.usageReport(tb)
	for _, want := range []string{"context", "50.0k", "200k", "25%", "cost", "$1.25"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage report missing %q:\n%s", want, out)
		}
	}
}
