package tui

import (
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// TestContextOccupancyDerivesFromTheThreeCounters locks the derivation: the wire
// carries no "context used" field, only the three counters that make it up.
func TestContextOccupancyDerivesFromTheThreeCounters(t *testing.T) {
	m := newTestModel()
	m.route(RoutedEvent{
		Conv:  contracts.Conversation{ID: "a"},
		Event: contracts.Event{T: "reply", Done: true, TokensIn: 30_000, CacheRead: 12_000, CacheCreate: 3_000},
	})
	if got := m.tabs["a"].ctxTokens; got != 45_000 {
		t.Fatalf("ctxTokens = %d, want 45000", got)
	}
}

// TestContextLimitFollowsTheVendorAndTheEnv checks the denominator: a per-vendor
// constant, overridable when a model's window moves under us.
func TestContextLimitFollowsTheVendorAndTheEnv(t *testing.T) {
	if got := contextLimit("claude"); got != 200_000 {
		t.Errorf("claude limit = %d", got)
	}
	if got := contextLimit("codex"); got != 400_000 {
		t.Errorf("codex limit = %d", got)
	}
	if got := contextLimit("nobody-ships-this"); got != defaultContextLimit {
		t.Errorf("unknown vendor must fall back: %d", got)
	}
	t.Setenv(contextLimitEnv, "12345")
	if got := contextLimit("claude"); got != 12345 {
		t.Errorf("the env must win: %d", got)
	}
}

// TestContextPaliersColourDifferently is the whole point of the gauge: a window
// that is fine, one worth planning around and one about to compact must not read
// as the same line.
func TestContextPaliersColourDifferently(t *testing.T) {
	calm := renderContext(40_000, "claude")   // 20%
	warn := renderContext(170_000, "claude")  // 85%
	alarm := renderContext(195_000, "claude") // 97.5%
	if calm == warn || warn == alarm || calm == alarm {
		t.Fatalf("the three paliers must differ: %q %q %q", calm, warn, alarm)
	}
	if warn != warnStyle.Render("170.0k/200k") {
		t.Fatalf("the warn palier must take the warn style: %q", warn)
	}
	if alarm != redStyle.Render("195.0k/200k") {
		t.Fatalf("the alarm palier must take the Herrscher red: %q", alarm)
	}
	if renderContext(0, "claude") != "" {
		t.Fatal("an unmeasured context must render nothing")
	}
}

// TestCostAccumulatesAcrossTurns is the bug this replaces: lastCost was
// overwritten every turn, so a six-hour session showed the price of its last one.
func TestCostAccumulatesAcrossTurns(t *testing.T) {
	m := newTestModel()
	for _, c := range []float64{0.01, 0.02, 0.03} {
		m.route(RoutedEvent{
			Conv:  contracts.Conversation{ID: "a"},
			Event: contracts.Event{T: "reply", Done: true, Cost: c},
		})
	}
	if got := m.tabs["a"].costTotal; got < 0.0599 || got > 0.0601 {
		t.Fatalf("costTotal = %v, want 0.06", got)
	}
	if got := m.tabs["a"].lastCost; got != 0.03 {
		t.Fatalf("lastCost must stay the last turn's: %v", got)
	}
}

// TestStatusBarReducesWhenNarrow keeps the two necessary segments and drops the
// merely useful ones rather than overflowing the row.
func TestStatusBarReducesWhenNarrow(t *testing.T) {
	m := newTestModel()
	tb := m.ensureTab("alpha")
	tb.ctxTokens = 40_000
	tb.costTotal = 1.23
	tb.openedAt = time.Now().Add(-90 * time.Minute)

	wide := m.statusBar(tb, 120)
	narrow := m.statusBar(tb, 40)
	for _, want := range []string{"alpha", "40.0k"} {
		if !strings.Contains(narrow, want) {
			t.Errorf("the narrow bar must keep %q: %q", want, narrow)
		}
	}
	if strings.Contains(narrow, "$1.23") {
		t.Errorf("the narrow bar must drop the cost: %q", narrow)
	}
	if !strings.Contains(wide, "$1.23") || !strings.Contains(wide, "1h30m") {
		t.Errorf("the wide bar must carry cost and age: %q", wide)
	}
}

func TestFormatDuration(t *testing.T) {
	for _, c := range []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"},
		{3 * time.Hour, "3h00m"},
		{95 * time.Minute, "1h35m"},
	} {
		if got := formatDuration(c.in); got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
