package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

func compactRecord(at time.Time, vendor, account string, entry Compaction) Record {
	return Record{Time: at, Session: "s1", Vendor: vendor, Account: account, Compaction: &entry}
}

func outcomeCounts(outcomes ...contracts.CompactOutcome) map[contracts.CompactOutcome]int {
	counts := make(map[contracts.CompactOutcome]int)
	for _, outcome := range outcomes {
		counts[outcome]++
	}
	return counts
}

func jsonOf(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %+v: %v", value, err)
	}
	return string(data)
}

func TestSummarizeAggregatesCompactionsPerStage(t *testing.T) {
	at := testNow().Add(-time.Hour)
	tests := []struct {
		name    string
		records []Record
		key     AccountKey
		want    []StageUsage
	}{
		{
			name:    "no record gives no stage",
			records: []Record{{Time: at, Model: "opus", In: 10, Cost: 1}},
			want:    []StageUsage{},
		},
		{
			name: "runs of one stage are summed and its outcomes counted",
			records: []Record{
				compactRecord(at, "claude", "", Compaction{Stage: "jev", Outcome: contracts.CompactDone, Calls: 4, Kept: 1, Truncated: 2, Pruned: 1, CharsBefore: 1000, CharsAfter: 250, TokensRemoved: 300, Requests: 2, RequestTokens: 900, Duration: 2 * time.Second}),
				compactRecord(at, "claude", "", Compaction{Stage: "jev", Outcome: contracts.CompactFailed, Calls: 2, Kept: 2, CharsBefore: 500, CharsAfter: 500, Requests: 1, RequestTokens: 100, Duration: time.Second}),
			},
			want: []StageUsage{{
				Stage: "jev", Runs: 2,
				Outcomes: outcomeCounts(contracts.CompactDone, contracts.CompactFailed),
				Calls:    6, Kept: 3, Truncated: 2, Pruned: 1,
				CharsBefore: 1500, CharsAfter: 750,
				TokensRemoved: 300, Requests: 3, RequestTokens: 1000, Duration: 3 * time.Second,
			}},
		},
		{
			name: "stages keep the order they first ran in",
			records: []Record{
				compactRecord(at, "claude", "", Compaction{Stage: "jev", Outcome: contracts.CompactShort}),
				compactRecord(at, "claude", "", Compaction{Stage: "backend", Outcome: contracts.CompactDone}),
				compactRecord(at, "claude", "", Compaction{Stage: "jev", Outcome: contracts.CompactSkipped}),
			},
			want: []StageUsage{
				{Stage: "jev", Runs: 2, Outcomes: outcomeCounts(contracts.CompactShort, contracts.CompactSkipped)},
				{Stage: "backend", Runs: 1, Outcomes: outcomeCounts(contracts.CompactDone)},
			},
		},
		{
			name: "the key keeps only the account it names",
			records: []Record{
				compactRecord(at, "claude", "", Compaction{Stage: "jev", Outcome: contracts.CompactDone}),
				compactRecord(at, "codex", "work", Compaction{Stage: "jev", Outcome: contracts.CompactDryRun}),
			},
			key: AccountKey{Vendor: "codex", Account: "work"},
			want: []StageUsage{
				{Stage: "jev", Runs: 1, Outcomes: outcomeCounts(contracts.CompactDryRun)},
			},
		},
		{
			name: "a run older than the period is left out",
			records: []Record{
				compactRecord(testNow().Add(-30*24*time.Hour), "claude", "", Compaction{Stage: "jev", Outcome: contracts.CompactDone}),
			},
			want: []StageUsage{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Summarize(tc.records, Range7d, testNow(), tc.key).Stages
			if jsonOf(t, got) != jsonOf(t, tc.want) {
				t.Fatalf("Stages = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSummarizeLeavesCompactionRunsOutOfTheSpend(t *testing.T) {
	records := []Record{
		{Time: testNow(), Session: "chat", Model: "opus", In: 10, Cost: 2},
		compactRecord(testNow(), "claude", "", Compaction{Stage: "jev", Outcome: contracts.CompactDone, Requests: 1}),
	}
	spend := Summarize(records, Range24h, testNow(), AccountKey{})
	if spend.Sessions != 1 || spend.Cost != 2 || spend.Tokens.In != 10 || len(spend.Models) != 1 {
		t.Fatalf("spend = %+v, want only the turn counted, the compaction run adds no session, cost or model", spend)
	}
	if len(spend.Stages) != 1 || spend.Stages[0].Requests != 1 {
		t.Fatalf("Stages = %+v, want the compaction run in its stage", spend.Stages)
	}
}

func TestStageUsageDerivedValues(t *testing.T) {
	tests := []struct {
		name        string
		stage       StageUsage
		wantRemoved float64
		wantAverage time.Duration
	}{
		{name: "no characters found removes nothing", stage: StageUsage{}, wantRemoved: 0, wantAverage: 0},
		{name: "a weighted share of the characters", stage: StageUsage{Runs: 2, CharsBefore: 1000, CharsAfter: 250, Duration: 3 * time.Second}, wantRemoved: 75, wantAverage: 1500 * time.Millisecond},
		{name: "characters that grew are a negative share", stage: StageUsage{CharsBefore: 100, CharsAfter: 150}, wantRemoved: -50, wantAverage: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.stage.CharsRemoved(); got != tc.wantRemoved {
				t.Fatalf("CharsRemoved = %v, want %v", got, tc.wantRemoved)
			}
			if got := tc.stage.AverageDuration(); got != tc.wantAverage {
				t.Fatalf("AverageDuration = %v, want %v", got, tc.wantAverage)
			}
		})
	}
}

func TestLedgerReadsAnOldLedgerWithoutCompactionEntries(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCost float64
		wantRuns int
	}{
		{
			name:     "a file written before compaction was recorded",
			body:     `{"time":"2026-11-08T10:00:00Z","session":"a","vendor":"claude","model":"opus","in":3,"cost":1.5}` + "\n",
			wantCost: 1.5,
		},
		{
			name: "a compaction line beside turns",
			body: `{"time":"2026-11-08T10:00:00Z","session":"a","vendor":"claude","model":"opus","in":3,"cost":1.5}` + "\n" +
				`{"time":"2026-11-08T11:00:00Z","session":"a","vendor":"claude","compaction":{"stage":"jev","outcome":"done","requests":2}}` + "\n",
			wantCost: 1.5,
			wantRuns: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "2026-11-08.jsonl"), []byte(tc.body), fileMode); err != nil {
				t.Fatalf("seed: %v", err)
			}
			ledger := Ledger{Dir: dir, Now: clock(testNow())}
			records, err := ledger.Records(Range7d)
			if err != nil {
				t.Fatalf("Records: %v", err)
			}
			spend := Summarize(records, Range7d, testNow(), AccountKey{})
			if spend.Cost != tc.wantCost {
				t.Fatalf("Cost = %v, want %v", spend.Cost, tc.wantCost)
			}
			if got := len(spend.Stages); got != tc.wantRuns {
				t.Fatalf("stages = %d, want %d", got, tc.wantRuns)
			}
		})
	}
}

func TestCompactionRecordRoundTripsThroughTheLedger(t *testing.T) {
	for _, outcome := range contracts.CompactOutcomes() {
		t.Run(outcome.String(), func(t *testing.T) {
			ledger := Ledger{Dir: t.TempDir(), Now: clock(testNow())}
			want := compactRecord(testNow(), "claude", "", Compaction{Stage: "jev", Outcome: outcome, Calls: 3, CharsBefore: 40, CharsAfter: 10, Duration: time.Second})
			if err := ledger.Append([]Record{want}); err != nil {
				t.Fatalf("Append: %v", err)
			}
			got, err := ledger.Records(Range24h)
			if err != nil {
				t.Fatalf("Records: %v", err)
			}
			if len(got) != 1 || jsonOf(t, got[0].Compaction) != jsonOf(t, want.Compaction) {
				t.Fatalf("Records = %+v, want the compaction %+v back", got, want.Compaction)
			}
		})
	}
}

func TestLastCompactionsKeepsTheLatestRunOfEachStage(t *testing.T) {
	early := testNow().Add(-2 * time.Hour)
	late := testNow().Add(-time.Hour)
	tests := []struct {
		name    string
		records []Record
		want    map[string]LastCompaction
	}{
		{name: "no compaction", records: []Record{{Time: late, Model: "opus"}}, want: make(map[string]LastCompaction)},
		{
			name: "the later run wins even when it is listed first",
			records: []Record{
				compactRecord(late, "claude", "", Compaction{Stage: "jev", Outcome: contracts.CompactFailed}),
				compactRecord(early, "claude", "", Compaction{Stage: "jev", Outcome: contracts.CompactDone}),
			},
			want: map[string]LastCompaction{"jev": {At: late, Outcome: contracts.CompactFailed}},
		},
		{
			name: "each stage keeps its own run",
			records: []Record{
				compactRecord(early, "claude", "", Compaction{Stage: "backend", Outcome: contracts.CompactShort}),
				compactRecord(late, "claude", "", Compaction{Stage: "jev", Outcome: contracts.CompactDone}),
			},
			want: map[string]LastCompaction{
				"backend": {At: early, Outcome: contracts.CompactShort},
				"jev":     {At: late, Outcome: contracts.CompactDone},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := LastCompactions(tc.records); jsonOf(t, got) != jsonOf(t, tc.want) {
				t.Fatalf("LastCompactions = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCompactionLinesRendersEachStage(t *testing.T) {
	stage := StageUsage{
		Stage: "jev", Runs: 2,
		Outcomes: outcomeCounts(contracts.CompactDone, contracts.CompactFailed),
		Calls:    6, Kept: 3, Truncated: 2, Pruned: 1,
		CharsBefore: 1000, CharsAfter: 250,
		TokensRemoved: 1200, Requests: 3, RequestTokens: 4000, Duration: 3 * time.Second,
	}
	tests := []struct {
		name     string
		snapshot Snapshot
		lang     i18n.Language
		want     []string
	}{
		{
			name:     "english stage block",
			snapshot: Snapshot{Spend: Spend{Stages: []StageUsage{stage}}},
			lang:     i18n.English,
			want:     []string{"jev", "2 compactions", "done 1", "failed 1", "6 calls examined, 3 kept, 2 truncated, 1 pruned", "75 % of the characters removed", "about 1.2 k tokens removed", "3 requests", "average 1.5 s"},
		},
		{
			name:     "french stage block",
			snapshot: Snapshot{Spend: Spend{Stages: []StageUsage{stage}}},
			lang:     i18n.French,
			want:     []string{"jev", "2 compactions", "terminée 1", "échouée 1", "6 appels examinés", "75 %", "1,2 k jetons retirés", "durée moyenne 1,5 s"},
		},
		{
			name:     "no stage says so",
			snapshot: Snapshot{},
			lang:     i18n.English,
			want:     []string{"no compaction in this period"},
		},
		{
			name:     "a truncated ledger shows the partial notice before the stages",
			snapshot: Snapshot{LedgerTruncated: true, Spend: Spend{Stages: []StageUsage{stage}}},
			lang:     i18n.English,
			want:     []string{"partial", "jev"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines, err := CompactionLines(tc.snapshot, tc.lang, termpaint.Palette{})
			if err != nil {
				t.Fatalf("CompactionLines: %v", err)
			}
			body := strings.Join(lines, "\n")
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Fatalf("lines = %q, want them to contain %q", body, want)
				}
			}
		})
	}
}
