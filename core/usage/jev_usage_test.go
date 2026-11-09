package usage

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

func stageRecord(at time.Time, vendor, session, passage, stage string, outcome contracts.CompactOutcome) Record {
	return Record{Time: at, Session: session, Vendor: vendor, Compaction: &Compaction{Stage: stage, Passage: passage, Outcome: outcome}}
}

func TestJevUsageCountsRunsSuccessesAndSessionsBehind(t *testing.T) {
	at := testNow().Add(-time.Hour)
	records := []Record{
		stageRecord(at, "claude", "s1", "p1", jevStage, contracts.CompactDone),
		stageRecord(at, "claude", "s1", "p1", contracts.CompactStageBackend, contracts.CompactShort),
		stageRecord(at, "claude", "s2", "p2", jevStage, contracts.CompactFailed),
		stageRecord(at, "claude", "s3", "", jevStage, contracts.CompactShort),
		stageRecord(at, "claude", "s3", "", contracts.CompactStageBackend, contracts.CompactDone),
		stageRecord(at, "claude", "s4", "p4", contracts.CompactStageBackend, contracts.CompactDone),
		stageRecord(at, "codex", "s5", "p5", jevStage, contracts.CompactDone),
		stageRecord(at, "codex", "s5", "p5", contracts.CompactStageBackend, contracts.CompactDone),
		stageRecord(at, "claude", "s6", "p6", jevStage, contracts.CompactSkipped),
		stageRecord(at, "claude", "s6", "p6", contracts.CompactStageBackend, contracts.CompactDone),
		stageRecord(at, "claude", "s7", "p7", jevStage, contracts.CompactDryRun),
		stageRecord(at, "claude", "s7", "p7", contracts.CompactStageBackend, contracts.CompactDone),
		stageRecord(at, "claude", "s8", "p8", jevStage, contracts.CompactDone),
		stageRecord(at, "claude", "s8", "p8", contracts.CompactStageBackend, contracts.CompactSkipped),
		stageRecord(at, "claude", "s9", "p9", jevStage, contracts.CompactFailed),
		stageRecord(at, "claude", "s9", "p9", contracts.CompactStageBackend, contracts.CompactDryRun),
	}
	tests := []struct {
		name string
		key  AccountKey
		want JevUsage
	}{
		{
			name: "every vendor counted when the key is zero",
			want: JevUsage{Runs: 6, Succeeded: 4, Sessions: 6, Behind: 3},
		},
		{
			name: "only the vendor of the key counted",
			key:  AccountKey{Vendor: "claude"},
			want: JevUsage{Runs: 5, Succeeded: 3, Sessions: 5, Behind: 2},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := jevUsages(records, time.Time{}, tc.key); got != tc.want {
				t.Fatalf("jevUsages = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestJevUsageDoesNotDependOnTheOrderOfTheRecords(t *testing.T) {
	at := testNow().Add(-time.Hour)
	records := []Record{
		stageRecord(at, "claude", "s1", "p1", jevStage, contracts.CompactDone),
		stageRecord(at, "claude", "s1", "p1", contracts.CompactStageBackend, contracts.CompactShort),
		stageRecord(at, "claude", "s2", "p2", jevStage, contracts.CompactFailed),
		stageRecord(at, "claude", "s2", "p2", contracts.CompactStageBackend, contracts.CompactDone),
	}
	reversed := slices.Clone(records)
	slices.Reverse(reversed)
	want := JevUsage{Runs: 2, Succeeded: 1, Sessions: 2, Behind: 2}
	if got := jevUsages(records, time.Time{}, AccountKey{}); got != want {
		t.Fatalf("in order: jevUsages = %+v, want %+v", got, want)
	}
	if got := jevUsages(reversed, time.Time{}, AccountKey{}); got != want {
		t.Fatalf("reversed: jevUsages = %+v, want %+v", got, want)
	}
}

func TestJevUsageShares(t *testing.T) {
	tests := []struct {
		name      string
		usage     JevUsage
		succeeded float64
		behind    float64
	}{
		{name: "nothing ran", usage: JevUsage{}, succeeded: 0, behind: 0},
		{name: "three of four succeeded, one session of two behind", usage: JevUsage{Runs: 4, Succeeded: 3, Sessions: 2, Behind: 1}, succeeded: 75, behind: 50},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.usage.SucceededShare() != tc.succeeded || tc.usage.BehindShare() != tc.behind {
				t.Fatalf("shares = %v, %v, want %v, %v", tc.usage.SucceededShare(), tc.usage.BehindShare(), tc.succeeded, tc.behind)
			}
		})
	}
}

func TestCompactionsFollowTheAccountFilterLikeTurns(t *testing.T) {
	at := testNow().Add(-time.Hour)
	legacy := stageRecord(at, "", "s1", "", jevStage, contracts.CompactDone)
	pro := stageRecord(at, "claude", "s1", "", jevStage, contracts.CompactDone)
	pro.Account = "pro"
	perso := stageRecord(at, "claude", "s1", "", jevStage, contracts.CompactDone)
	perso.Account = "perso"
	records := []Record{legacy, pro, perso}
	tests := []struct {
		name string
		key  AccountKey
		want int
	}{
		{name: "no filter keeps the unaccounted record too", want: 3},
		{name: "an account filter leaves the unaccounted record out", key: AccountKey{Vendor: "claude", Account: "pro"}, want: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spend := Summarize(records, Range7d, testNow(), tc.key)
			if got := spend.Jev.Runs; got != tc.want {
				t.Fatalf("Jev.Runs = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPriceCompactionsPricesTheRemovedTokensAtTheCacheReadRate(t *testing.T) {
	const priceList = `{"claude-opus-4-1": {"input_cost_per_token": 0.000015, "output_cost_per_token": 0.000075, "cache_read_input_token_cost": 0.0000015}}`
	book, err := ParsePrices([]byte(priceList))
	if err != nil {
		t.Fatalf("ParsePrices: %v", err)
	}
	const removed = 1000
	tests := []struct {
		name         string
		book         PriceBook
		record       Record
		wantSaved    float64
		wantUnpriced bool
	}{
		{
			name:      "a known model saves its removed tokens at the cache read rate",
			book:      book,
			record:    Record{Model: "claude-opus-4-1", Compaction: &Compaction{Stage: jevStage, TokensRemoved: removed}},
			wantSaved: 0.0015,
		},
		{
			name:         "an unknown model is unpriced, never zero dollars",
			book:         book,
			record:       Record{Model: "inconnu", Compaction: &Compaction{Stage: jevStage, TokensRemoved: removed}},
			wantUnpriced: true,
		},
		{
			name:         "no price list at all leaves every compaction unpriced",
			record:       Record{Model: "claude-opus-4-1", Compaction: &Compaction{Stage: jevStage, TokensRemoved: removed}},
			wantUnpriced: true,
		},
		{
			name:   "a record without compaction is left alone",
			book:   book,
			record: Record{Model: "claude-opus-4-1", In: 10},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			records := []Record{tc.record}
			tc.book.PriceCompactions(records)
			got := records[0]
			if math.Abs(got.Saved-tc.wantSaved) > 1e-12 || got.Unpriced != tc.wantUnpriced {
				t.Fatalf("saved = %v, unpriced = %t, want %v, %t", got.Saved, got.Unpriced, tc.wantSaved, tc.wantUnpriced)
			}
		})
	}
}

func assertCompactionLines(t *testing.T, stage StageUsage, jev JevUsage, lang i18n.Language, want, notWant []string) {
	t.Helper()
	snapshot := Snapshot{Spend: Spend{Stages: []StageUsage{stage}, Jev: jev}}
	lines, err := CompactionLines(snapshot, lang, termpaint.Palette{})
	if err != nil {
		t.Fatalf("CompactionLines: %v", err)
	}
	body := strings.Join(lines, "\n")
	for _, text := range want {
		if !strings.Contains(body, text) {
			t.Fatalf("lines = %q, want them to contain %q", body, text)
		}
	}
	for _, text := range notWant {
		if strings.Contains(body, text) {
			t.Fatalf("lines = %q, want them not to contain %q", body, text)
		}
	}
}

func TestCompactionLinesShowsTheJevShareAndTheSaving(t *testing.T) {
	stage := StageUsage{Stage: jevStage, Runs: 2, Outcomes: outcomeCounts(contracts.CompactDone, contracts.CompactFailed), Saved: 1.5}
	jev := JevUsage{Runs: 4, Succeeded: 3, Sessions: 2, Behind: 1}
	tests := []struct {
		name    string
		stage   StageUsage
		jev     JevUsage
		lang    i18n.Language
		want    []string
		notWant []string
	}{
		{
			name:  "english shares and a priced saving",
			stage: stage,
			jev:   jev,
			lang:  i18n.English,
			want:  []string{"Jev succeeded 75 % (3 of 4)", "backend behind Jev in 50 % of sessions (1 of 2)", "estimated saving $1.50"},
		},
		{
			name:    "unknown price says so and shows no amount",
			stage:   StageUsage{Stage: jevStage, Runs: 2, Outcomes: outcomeCounts(contracts.CompactDone), UnpricedRuns: 2},
			lang:    i18n.English,
			want:    []string{"saving: unknown price"},
			notWant: []string{"$"},
		},
		{
			name:  "partly unknown price shows the amount and the count",
			stage: StageUsage{Stage: jevStage, Runs: 2, Outcomes: outcomeCounts(contracts.CompactDone), Saved: 1.5, UnpricedRuns: 1},
			lang:  i18n.English,
			want:  []string{"estimated saving $1.50", "unknown price for 1 of 2"},
		},
		{
			name:    "no jev run means no jev share line",
			stage:   stage,
			lang:    i18n.English,
			notWant: []string{"Jev succeeded"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertCompactionLines(t, tc.stage, tc.jev, tc.lang, tc.want, tc.notWant)
		})
	}
}
