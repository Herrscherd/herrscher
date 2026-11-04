package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

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

func TestContextFormatsTheSameNumbersEverywhere(t *testing.T) {
	if got := formatContext(82_000, 200_000); got != "82k/200k" {
		t.Errorf("formatContext = %q, want 82k/200k", got)
	}
	if got := formatContext(500, 200_000); got != "500/200k" {
		t.Errorf("a count under a thousand stays plain: %q", got)
	}
	if got := contextPercent(82_000, 200_000); got != "41%" {
		t.Errorf("contextPercent = %q, want 41%%", got)
	}
}

func TestContextPaliersTakeTheTierThresholds(t *testing.T) {
	fg := func(ratio float64) string {
		color, ok := palierStyle(ratio, testStyles()).GetForeground().(lipgloss.Color)
		if !ok {
			t.Fatalf("palier %v has no colour", ratio)
		}
		return string(color)
	}
	cases := []struct {
		ratio float64
		want  string
	}{
		{0.10, string(defaultTheme().Colors.Dim)},
		{0.79, string(defaultTheme().Colors.Dim)},
		{0.80, string(defaultTheme().Colors.Wait)},
		{0.94, string(defaultTheme().Colors.Wait)},
		{0.95, string(defaultTheme().Colors.Err)},
		{1.00, string(defaultTheme().Colors.Err)},
	}
	for _, c := range cases {
		if got := fg(c.ratio); got != c.want {
			t.Errorf("palier at %v = %s, want %s", c.ratio, got, c.want)
		}
	}
}

func TestContextGaugeFillsTenCells(t *testing.T) {
	gauge := contextGauge(0.5, testStyles())
	if filled := strings.Count(gauge, unicodeGlyphs().gaugeFull); filled != 5 {
		t.Errorf("half a window must fill 5 cells, got %d in %q", filled, gauge)
	}
	if empty := strings.Count(gauge, unicodeGlyphs().gaugeEmpty); empty != 5 {
		t.Errorf("half a window must leave 5 cells, got %d in %q", empty, gauge)
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
	if !strings.Contains(bar, "opus-5 · high") {
		t.Errorf("la barre doit nommer le modele et l'effort: %q", bar)
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

	if !strings.Contains(m.statusBar(tb, 120), "96k/200k") {
		t.Fatalf("un onglet sans tour vu doit lire la mesure du hub: %q", m.statusBar(tb, 120))
	}
	tb.ctxTokens = 120_000
	if !strings.Contains(m.statusBar(tb, 120), "120k/200k") {
		t.Fatalf("un tour vu passer doit primer sur la fiche: %q", m.statusBar(tb, 120))
	}
}

func TestContextLeftCountsDownToTheCompaction(t *testing.T) {
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
}

func barModelAt(t *testing.T, dir string) (*model, *tab) {
	t.Helper()
	be := &countingBackend{}
	be.sessions = []contracts.SessionInfo{{
		Name: "a", ChannelID: "c1", Vendor: "claude", Model: "claude-opus-5", Effort: "high",
		Project: "herrscher", Dir: dir,
	}}
	m := newModel(be)
	tb := m.ensureTab("c1")
	m.active = "c1"
	tb.ctxTokens = 82_000
	tb.costTotal = 1.42
	return m, tb
}

func writeWorktreeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitDir := filepath.Join(root, "gitdir")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/feat/tui-v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "wt")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestStatusBarFitsEveryWidth(t *testing.T) {
	m, tb := barModelAt(t, writeWorktreeRepo(t))
	tb.busy = true
	m.bar.setQueued(2)
	m.bar.setBackground(1)
	for _, width := range []int{60, 80, 120, 200} {
		bar := m.statusBar(tb, width)
		if got := lipgloss.Width(bar); got > width {
			t.Errorf("width %d: bar is %d cells wide: %q", width, got, bar)
		}
		if !strings.Contains(bar, "Man") {
			t.Errorf("width %d: the mode must always show: %q", width, bar)
		}
		if !strings.Contains(bar, "41%") {
			t.Errorf("width %d: the context percentage must always show: %q", width, bar)
		}
	}
}

func TestStatusBarDropsItsFieldsByPriorityAsItNarrows(t *testing.T) {
	m, tb := barModelAt(t, writeWorktreeRepo(t))
	m.bar.setBackground(1)
	wide := m.statusBar(tb, 200)
	for _, want := range []string{"Manuel", "opus-5", "feat/tui-v2", "(worktree)", "82k/200k", "$1.42", "1 en fond"} {
		if !strings.Contains(wide, want) {
			t.Errorf("200 columns must carry %q: %q", want, wide)
		}
	}
	mid := m.statusBar(tb, 60)
	if strings.Contains(mid, "1 en fond") {
		t.Errorf("the background tasks go first when narrow: %q", mid)
	}
	if !strings.Contains(mid, "41%") {
		t.Errorf("60 columns keeps the percentage: %q", mid)
	}
}

func TestStatusBarShowsTheSpinnerWhileBusy(t *testing.T) {
	m, tb := barModelAt(t, "")
	tb.busy = true
	tb.startedAt = time.Now()
	if bar := m.statusBar(tb, 200); !strings.Contains(bar, "échap pour interrompre") {
		t.Errorf("a busy tab must keep the working hint in the bar: %q", bar)
	}
}

func TestStatusBarRendersTheModeChip(t *testing.T) {
	m, tb := barModelAt(t, "")
	if bar := m.statusBar(tb, 120); !strings.Contains(bar, "Manuel") {
		t.Errorf("the mode chip must lead with the session mode: %q", bar)
	}
	tb.setMode(modeAuto)
	if bar := m.statusBar(tb, 120); !strings.Contains(bar, "Auto") {
		t.Errorf("the session mode must reach the bar: %q", bar)
	}
}
