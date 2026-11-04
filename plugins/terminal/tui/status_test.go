package tui

import (
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestContextOccupancyDerivesFromTheThreeCounters(t *testing.T) {
	m := newTestModel()
	m.route(RoutedEvent{
		Conv:  contracts.Conversation{ID: "a"},
		Event: contracts.Event{T: "chunk", Text: "…", TokensIn: 30_000, CacheRead: 12_000, CacheCreate: 3_000},
	})
	if got := m.tabs["a"].ctxTokens; got != 45_000 {
		t.Fatalf("ctxTokens = %d, want 45000", got)
	}
}

func TestContextOccupancyIgnoresTheTurnTotals(t *testing.T) {
	m := newTestModel()
	conv := contracts.Conversation{ID: "a"}
	m.route(RoutedEvent{Conv: conv, Event: contracts.Event{T: "chunk", Text: "…", TokensIn: 40_000, CacheRead: 50_000}})
	m.route(RoutedEvent{Conv: conv, Event: contracts.Event{T: "reply", Done: true, Text: "done", TokensIn: 600_000, CacheRead: 652_200}})
	if got := m.tabs["a"].ctxTokens; got != 90_000 {
		t.Fatalf("ctxTokens = %d, want the last mid-turn reading (90000)", got)
	}
}

func TestContextOccupancyTakesTheTotalsOfAOneMessageTurn(t *testing.T) {
	m := newTestModel()
	conv := contracts.Conversation{ID: "a"}
	m.route(RoutedEvent{Conv: conv, Event: contracts.Event{T: "chunk", Text: "hi"}})
	m.route(RoutedEvent{Conv: conv, Event: contracts.Event{T: "reply", Done: true, Text: "hi", TokensIn: 12_000, CacheRead: 3_000}})
	if got := m.tabs["a"].ctxTokens; got != 15_000 {
		t.Fatalf("ctxTokens = %d, want the single message's prompt (15000)", got)
	}
	m.route(RoutedEvent{Conv: conv, Event: contracts.Event{T: "chunk", Text: "…", TokensIn: 20_000}})
	m.route(RoutedEvent{Conv: conv, Event: contracts.Event{T: "reply", Done: true, Text: "done", TokensIn: 900_000}})
	if got := m.tabs["a"].ctxTokens; got != 20_000 {
		t.Fatalf("ctxTokens = %d, want the mid-turn reading (20000)", got)
	}
}

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

func TestContextPaliersColourDifferently(t *testing.T) {
	calm := renderContext(40_000, "claude", testStyles())
	warn := renderContext(170_000, "claude", testStyles())
	alarm := renderContext(195_000, "claude", testStyles())
	if calm == warn || warn == alarm || calm == alarm {
		t.Fatalf("the three paliers must differ: %q %q %q", calm, warn, alarm)
	}
	if warn != testStyles().warn.Render("170.0k/200k") {
		t.Fatalf("the warn palier must take the warn style: %q", warn)
	}
	if alarm != testStyles().red.Render("195.0k/200k") {
		t.Fatalf("the alarm palier must take the Herrscher red: %q", alarm)
	}
	if renderContext(0, "claude", testStyles()) != "" {
		t.Fatal("an unmeasured context must render nothing")
	}
}

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

func TestStatusBarNamesTheModelAndTheEffort(t *testing.T) {
	be := &countingBackend{}
	be.sessions = []contracts.SessionInfo{{
		Name: "a", ChannelID: "c1", Vendor: "claude", Model: "claude-opus-5", Effort: "high",
	}}
	m := newModel(be)
	tb := m.ensureTab("c1")
	m.active = "c1"

	bar := m.statusBar(tb, 120)
	if !strings.Contains(bar, "opus-5") {
		t.Errorf("la barre doit nommer le modele: %q", bar)
	}
	if !strings.Contains(bar, "high") {
		t.Errorf("la barre doit nommer l'effort: %q", bar)
	}
	if strings.Contains(bar, "claude-opus-5") {
		t.Errorf("le vendeur mene deja la barre, le prefixe est redondant: %q", bar)
	}
}

func TestStatusBarFallsBackOnTheHubReading(t *testing.T) {
	be := &countingBackend{}
	be.sessions = []contracts.SessionInfo{{
		Name: "a", ChannelID: "c1", Vendor: "claude", ContextTokens: 96_000,
	}}
	m := newModel(be)
	tb := m.ensureTab("c1")
	m.active = "c1"

	if !strings.Contains(m.statusBar(tb, 120), "96.0k") {
		t.Fatalf("un onglet sans tour vu doit lire la mesure du hub: %q", m.statusBar(tb, 120))
	}
	tb.ctxTokens = 120_000
	if !strings.Contains(m.statusBar(tb, 120), "120.0k") {
		t.Fatalf("un tour vu passer doit primer sur la fiche: %q", m.statusBar(tb, 120))
	}
}

func TestGaugeCountsDownToTheCompaction(t *testing.T) {
	if got := contextLeft(0, 200_000); got != 1 {
		t.Errorf("une fenetre vide laisse tout le budget: %v", got)
	}
	if got := contextLeft(184_000, 200_000); got != 0 {
		t.Errorf("le seuil de compaction laisse zero: %v", got)
	}
	if got := contextLeft(200_000, 200_000); got != 0 {
		t.Errorf("au-dela du seuil le reste ne devient pas negatif: %v", got)
	}
	if got := contextLeft(92_000, 200_000); got < 0.49 || got > 0.51 {
		t.Errorf("la moitie du budget doit se lire 50 %%: %v", got)
	}
	wide := renderGauge(92_000, "claude", 120, testStyles())
	narrow := renderGauge(92_000, "claude", 70, testStyles())
	if !strings.Contains(wide, "reste 50 %") || !strings.Contains(wide, "avant compaction") {
		t.Errorf("une barre large doit epeler ce que le compte decompte: %q", wide)
	}
	if !strings.Contains(narrow, "reste 50 %") || strings.Contains(narrow, "avant compaction") {
		t.Errorf("une barre etroite garde le chiffre et lache la glose: %q", narrow)
	}
}
