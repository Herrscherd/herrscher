package host

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/usage"
)

type usageCapture struct {
	mu      sync.Mutex
	records []usage.Record
}

func (c *usageCapture) add(records []usage.Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, records...)
}

func (c *usageCapture) all() []usage.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.records
}

type statsCompactor struct {
	stats *contracts.CompactStats
	err   error
}

func (c statsCompactor) Compact(_ context.Context, _ contracts.CompactRequest, _ func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	return contracts.CompactResult{Stats: c.stats}, c.err
}

func stageStats(stage string, outcome contracts.CompactOutcome) contracts.CompactStats {
	return contracts.CompactStats{
		Stage:         stage,
		Outcome:       outcome,
		Calls:         3,
		Kept:          1,
		Truncated:     1,
		Pruned:        1,
		CharsBefore:   1000,
		CharsAfter:    200,
		TokensRemoved: 300,
		Requests:      1,
		RequestTokens: 900,
		Duration:      time.Second,
	}
}

func recordedCompaction(stage string, outcome contracts.CompactOutcome) *usage.Compaction {
	return &usage.Compaction{
		Stage:         stage,
		Outcome:       outcome,
		Calls:         3,
		Kept:          1,
		Truncated:     1,
		Pruned:        1,
		CharsBefore:   1000,
		CharsAfter:    200,
		TokensRemoved: 300,
		Requests:      1,
		RequestTokens: 900,
		Duration:      time.Second,
	}
}

func TestRecordCompactionKeepsOnlyValidStageStats(t *testing.T) {
	tests := []struct {
		name  string
		stats []contracts.CompactStats
		want  []*usage.Compaction
	}{
		{name: "no stats records nothing"},
		{
			name:  "a successful stage is recorded",
			stats: []contracts.CompactStats{stageStats("jev", contracts.CompactDone)},
			want:  []*usage.Compaction{recordedCompaction("jev", contracts.CompactDone)},
		},
		{
			name:  "a failed stage is recorded with its outcome",
			stats: []contracts.CompactStats{stageStats("jev", contracts.CompactFailed)},
			want:  []*usage.Compaction{recordedCompaction("jev", contracts.CompactFailed)},
		},
		{
			name: "a stage with an invalid outcome is left out, the others kept",
			stats: []contracts.CompactStats{
				stageStats("jev", 0),
				stageStats("backend", contracts.CompactShort),
			},
			want: []*usage.Compaction{recordedCompaction("backend", contracts.CompactShort)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var capture usageCapture
			d := &sessionDriver{name: "s1", sink: sessionSink{Usage: capture.add}}
			d.recordCompaction(tc.stats)
			assertRecorded(t, capture.all(), tc.want)
		})
	}
}

func assertRecorded(t *testing.T, records []usage.Record, want []*usage.Compaction) {
	t.Helper()
	if len(records) != len(want) {
		t.Fatalf("records = %+v, want %d", records, len(want))
	}
	for index := range records {
		if records[index].Session != "s1" || records[index].Time.IsZero() {
			t.Fatalf("record = %+v, want the session and the time of the run", records[index])
		}
		if jsonOf(t, records[index].Compaction) != jsonOf(t, want[index]) {
			t.Fatalf("compaction = %+v, want %+v", records[index].Compaction, want[index])
		}
	}
}

func jsonOf(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %+v: %v", value, err)
	}
	return string(data)
}

func TestRecordCompactionWithoutUsageSinkDoesNotPanic(_ *testing.T) {
	d := &sessionDriver{name: "s1"}
	d.recordCompaction([]contracts.CompactStats{stageStats("jev", contracts.CompactDone)})
}

type slowFailure struct {
	wait time.Duration
}

func (c slowFailure) Compact(_ context.Context, _ contracts.CompactRequest, _ func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	time.Sleep(c.wait)
	return contracts.CompactResult{}, errors.New("disque plein")
}

func TestAFailedStageWithoutStatsLeavesAFailedEntry(t *testing.T) {
	const wait = 20 * time.Millisecond
	stages := []contracts.CompactStage{{Name: "jev", Compactor: recordedStage{name: "jev", compactor: slowFailure{wait: wait}}}}
	result, err := contracts.RunCompactStack(context.Background(), stages, contracts.CompactRequest{}, nil)
	if err == nil {
		t.Fatal("RunCompactStack returned no error, want the stage failure")
	}
	if len(result.StageStats) != 1 {
		t.Fatalf("StageStats = %+v, want one failed entry", result.StageStats)
	}
	got := result.StageStats[0]
	if got.Stage != "jev" || got.Outcome != contracts.CompactFailed || got.Duration < wait {
		t.Fatalf("StageStats[0] = %+v, want a failed jev entry timed over the stage's run", got)
	}
}

func TestRecordedStageNamesTheStageAfterItsConfiguredName(t *testing.T) {
	stats := stageStats("autre", contracts.CompactDone)
	stages := []contracts.CompactStage{{Name: "jev", Compactor: recordedStage{name: "jev", compactor: statsCompactor{stats: &stats}}}}
	result, err := contracts.RunCompactStack(context.Background(), stages, contracts.CompactRequest{}, nil)
	if err != nil {
		t.Fatalf("RunCompactStack: %v", err)
	}
	if len(result.StageStats) != 1 || result.StageStats[0].Stage != "jev" {
		t.Fatalf("StageStats = %+v, want one entry named after the configured stage", result.StageStats)
	}
}

func TestCompactionStatsReachTheUsageLedgerThroughTheBridge(t *testing.T) {
	var capture usageCapture
	f := newConfiguredCompactFixture(t, "claude", compactPolicy(92), func(d *sessionDriver) {
		d.sink.Usage = capture.add
	})
	f.driver.requestCompaction("", contracts.Conversation{})
	nextFrame(t, f)
	stats := stageStats("backend", contracts.CompactShort)
	f.from <- mustAnswer(t, contracts.CompactResult{TokensAfter: 30_000, TokensMeasured: true, Note: "compacté", Stats: &stats})
	waitEmitted(t, f, eventCompactDone)
	records := capture.all()
	if len(records) != 1 || jsonOf(t, records[0].Compaction) != jsonOf(t, recordedCompaction("backend", contracts.CompactShort)) {
		t.Fatalf("records = %+v, want the bridge stage recorded as short", records)
	}
}

func TestAnUnwiredStageLeavesNoEntry(t *testing.T) {
	stages := []contracts.CompactStage{{Name: "jev", Compactor: recordedStage{name: "jev", compactor: historyStage{}}}}
	result, err := contracts.RunCompactStack(context.Background(), stages, contracts.CompactRequest{}, nil)
	if !errors.Is(err, contracts.ErrNilCompactor) {
		t.Fatalf("RunCompactStack = %v, want ErrNilCompactor", err)
	}
	if len(result.StageStats) != 0 {
		t.Fatalf("StageStats = %+v, want none for a stage that did not run", result.StageStats)
	}
}

func TestCompactionStageOutcomeReachesTheUsageLedger(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		outcome contracts.CompactOutcome
		kind    string
	}{
		{name: "a successful stage", outcome: contracts.CompactDone, kind: eventCompactDone},
		{name: "a failed stage", err: errors.New("boom"), outcome: contracts.CompactFailed, kind: eventCompactFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var capture usageCapture
			policy := compactPolicy(92, "custom", contracts.CompactStageBackend)
			stats := stageStats("custom", tc.outcome)
			policy.plugins["custom"] = statsCompactor{stats: &stats, err: tc.err}
			f := newConfiguredCompactFixture(t, "claude", policy, func(d *sessionDriver) {
				d.sink.Usage = capture.add
			})
			f.driver.requestCompaction("", contracts.Conversation{})
			if tc.err == nil {
				nextFrame(t, f)
				f.from <- measuredAnswer(t, 30_000)
			}
			waitEmitted(t, f, tc.kind)
			records := capture.all()
			if len(records) != 1 || jsonOf(t, records[0].Compaction) != jsonOf(t, recordedCompaction("custom", tc.outcome)) {
				t.Fatalf("records = %+v, want the custom stage recorded as %s", records, tc.outcome)
			}
		})
	}
}

func TestLastCompactionsReadsTheSevenDayWindow(t *testing.T) {
	now := time.Now().UTC()
	latest := now.Add(-time.Hour)
	ledger := usage.Ledger{Dir: t.TempDir(), Now: time.Now}
	if err := ledger.Append([]usage.Record{
		{Time: now.Add(-2 * time.Hour), Session: "s1", Compaction: &usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone}},
		{Time: latest, Session: "s1", Compaction: &usage.Compaction{Stage: "jev", Outcome: contracts.CompactFailed}},
		{Time: now.Add(-time.Minute), Session: "s1", Model: "opus", In: 5},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, ok := lastCompactions(ledger)["jev"]
	if !ok || got.Outcome != contracts.CompactFailed || !got.At.Equal(latest) {
		t.Fatalf("last jev run = %+v, %t, want the failed run at %v", got, ok, latest)
	}
}

func TestLastCompactionsKeepsTheReadableDaysBesideATruncatedOne(t *testing.T) {
	now := time.Now().UTC()
	dir := t.TempDir()
	ledger := usage.Ledger{Dir: dir, Now: time.Now}
	if err := ledger.Append([]usage.Record{
		{Time: now, Session: "s1", Compaction: &usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone}},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	oversized := filepath.Join(dir, now.AddDate(0, 0, -1).Format(time.DateOnly)+".jsonl")
	if err := os.WriteFile(oversized, nil, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Truncate(oversized, 64<<20+1); err != nil {
		t.Fatalf("grow: %v", err)
	}
	got, ok := lastCompactions(ledger)["jev"]
	if !ok || got.Outcome != contracts.CompactDone {
		t.Fatalf("last jev run = %+v, %t, want the run of the readable day", got, ok)
	}
}

func TestLastCompactionsIgnoresRunsOutsideTheProbeWindow(t *testing.T) {
	now := time.Now().UTC()
	ledger := usage.Ledger{Dir: t.TempDir(), Now: time.Now}
	if err := ledger.Append([]usage.Record{
		{Time: now.AddDate(0, 0, -30), Session: "s1", Compaction: &usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone}},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got, ok := lastCompactions(ledger)["jev"]; ok {
		t.Fatalf("last jev run = %+v, want none: the run is older than the probe window", got)
	}
}
