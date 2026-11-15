package usage

import (
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

func TestFormatTokensMovesToTheNextUnitWhenRoundingReachesIt(t *testing.T) {
	text := &renderer{lang: i18n.French}
	cases := map[int64]string{999: "999", 999950: "1,0 M", 1500: "1,5 k", 250_000_000: "250 M"}
	for count, want := range cases {
		if got := formatTokens(i18n.French, count, text); !strings.Contains(got, want) || strings.Contains(got, "1000") {
			t.Errorf("formatTokens(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestFormatPercentNamesATinyShare(t *testing.T) {
	if got := formatPercent(i18n.French, 0.01); got != "<0,1 %" {
		t.Fatalf("tiny share = %q", got)
	}
	if got := formatPercent(i18n.French, 0); strings.HasPrefix(got, "<") {
		t.Fatalf("zero share = %q", got)
	}
}

func TestDayLabelNamesTheMonth(t *testing.T) {
	day := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	if got := (&renderer{lang: i18n.French}).dayLabel(day); got != "21 oct." {
		t.Fatalf("french = %q", got)
	}
	if got := (&renderer{lang: i18n.English}).dayLabel(day); !strings.Contains(got, "21") || strings.Contains(got, "/") {
		t.Fatalf("english = %q", got)
	}
}

func spendSnapshot() Snapshot {
	start := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	buckets := make([]Bucket, 30)
	for index := range buckets {
		buckets[index] = Bucket{Start: start.AddDate(0, 0, index), Cost: float64(index)}
	}
	return Snapshot{Range: Range30d, PricesMissing: true, Spend: Spend{
		Cost: 435, Sessions: 3, Buckets: buckets,
		Tokens:   Tokens{In: 1000, Out: 2000, CacheRead: 3_000_000},
		Split:    CostSplit{In: 10, Out: 20, CacheRead: 400, CacheWrite: 5},
		Saved:    120,
		Models:   []Share{{Vendor: contracts.VendorClaude, Model: "claude-opus-4-1", Cost: 430}, {Model: "gpt-5", Cost: 5}},
		Accounts: []Share{{Vendor: contracts.VendorClaude, Cost: 435}},
	}}
}

func TestCostLinesSplitsTheCostByTypeAndShowsTheCache(t *testing.T) {
	lines, err := CostLines(spendSnapshot(), i18n.French, time.UTC, termpaint.Palette{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"PAR TYPE", "sortie $20,00", "3,0 M lus", "économie $120", "! "} {
		if !strings.Contains(joined, want) {
			t.Errorf("cost misses %q:\n%s", want, joined)
		}
	}
}

func TestCostPanelDrawsTheSummaryBesideTheChart(t *testing.T) {
	lines, err := CostPanel(spendSnapshot(), i18n.French, time.UTC, termpaint.Palette{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"$435,00", "Coût par jour", "opus-4-1", "┤", "┼", "10 oct.", "8 nov.", "Jetons", "économie cache $120"} {
		if !strings.Contains(joined, want) {
			t.Errorf("panel misses %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "claude-opus") {
		t.Errorf("panel keeps the vendor prefix:\n%s", joined)
	}
	for _, line := range lines {
		if termpaint.Width(line) > 100 {
			t.Errorf("line wider than the panel: %q", line)
		}
	}
}

func TestChartStepRoundsUpToANiceValue(t *testing.T) {
	cases := map[float64]float64{0: 1, 29: 10, 31: 20, 140: 50, 0.3: 0.1}
	for peak, want := range cases {
		if got := chartStep(peak); !near(got, want) {
			t.Errorf("chartStep(%v) = %v, want %v", peak, got, want)
		}
	}
}

func TestCardPaceWarnsOnlyWhenAheadOfThePace(t *testing.T) {
	text := &renderer{lang: i18n.French}
	limit := contracts.Limit{UsedPercent: 65}
	ahead := cardPace(limit, Pace{Shown: true, Delta: 43}, "35 %", text)
	if len(ahead) != 2 || !strings.Contains(ahead[0], paceUp) || !strings.Contains(ahead[1], "+43 %") {
		t.Fatalf("ahead = %q", ahead)
	}
	behind := cardPace(limit, Pace{Shown: true, Delta: -10}, "35 %", text)
	if len(behind) != 1 || !strings.Contains(behind[0], paceDown) {
		t.Fatalf("behind = %q", behind)
	}
	if exhausted := cardPace(contracts.Limit{UsedPercent: 100}, Pace{Shown: true, Delta: 5}, "0 %", text); len(exhausted) != 2 || strings.Contains(exhausted[1], "rythme") {
		t.Fatalf("exhausted = %q", exhausted)
	}
}
