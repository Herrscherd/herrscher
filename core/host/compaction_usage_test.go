package host

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/health"
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

func TestCompactionUsageBoundsWhatTheBackendSends(t *testing.T) {
	tests := []struct {
		name  string
		stats contracts.CompactStats
		want  usage.Compaction
	}{
		{
			name:  "a plausible stage is copied as it is",
			stats: stageStats("jev", contracts.CompactDone),
			want:  *recordedCompaction("jev", contracts.CompactDone),
		},
		{
			name:  "negative lengths and duration read as zero",
			stats: contracts.CompactStats{Stage: "jev", Outcome: contracts.CompactDone, CharsBefore: -5, CharsAfter: -3, Duration: -time.Second},
			want:  usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone},
		},
		{
			name:  "characters after are capped at the characters before",
			stats: contracts.CompactStats{Stage: "jev", Outcome: contracts.CompactDone, CharsBefore: 100, CharsAfter: 250},
			want:  usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone, CharsBefore: 100, CharsAfter: 100},
		},
		{
			name:  "negative characters before leave no characters after",
			stats: contracts.CompactStats{Stage: "jev", Outcome: contracts.CompactDone, CharsBefore: -10, CharsAfter: 40},
			want:  usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone},
		},
		{
			name:  "a negative call count reads as zero",
			stats: contracts.CompactStats{Stage: "jev", Outcome: contracts.CompactDone, Calls: -3},
			want:  usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone},
		},
		{
			name:  "a negative kept count reads as zero",
			stats: contracts.CompactStats{Stage: "jev", Outcome: contracts.CompactDone, Kept: -3},
			want:  usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone},
		},
		{
			name:  "a negative truncated count reads as zero",
			stats: contracts.CompactStats{Stage: "jev", Outcome: contracts.CompactDone, Truncated: -3},
			want:  usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone},
		},
		{
			name:  "a negative pruned count reads as zero",
			stats: contracts.CompactStats{Stage: "jev", Outcome: contracts.CompactDone, Pruned: -3},
			want:  usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone},
		},
		{
			name:  "a negative token count reads as zero",
			stats: contracts.CompactStats{Stage: "jev", Outcome: contracts.CompactDone, TokensRemoved: -300},
			want:  usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone},
		},
		{
			name:  "a negative request count reads as zero",
			stats: contracts.CompactStats{Stage: "jev", Outcome: contracts.CompactDone, Requests: -1},
			want:  usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone},
		},
		{
			name:  "a negative request token count reads as zero",
			stats: contracts.CompactStats{Stage: "jev", Outcome: contracts.CompactDone, RequestTokens: -900},
			want:  usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone},
		},
		{
			name:  "a duration beyond a day is capped at a day",
			stats: contracts.CompactStats{Stage: "jev", Outcome: contracts.CompactDone, Duration: 1000 * time.Hour},
			want:  usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone, Duration: maxCompactionDuration},
		},
		{
			name:  "a stage name beyond the cap is cut to its first runes",
			stats: contracts.CompactStats{Stage: strings.Repeat("é", 100), Outcome: contracts.CompactDone},
			want:  usage.Compaction{Stage: strings.Repeat("é", maxStageRunes), Outcome: contracts.CompactDone},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := compactionUsage(tc.stats); *got != tc.want {
				t.Fatalf("compactionUsage = %+v, want %+v", *got, tc.want)
			}
		})
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
			d.recordCompaction(tc.stats, nil)
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
		if !sameEntry(t, records[index].Compaction, want[index]) {
			t.Fatalf("compaction = %+v, want %+v", records[index].Compaction, want[index])
		}
	}
}

func sameEntry(t *testing.T, got, want *usage.Compaction) bool {
	t.Helper()
	if got == nil || got.Passage == "" {
		return false
	}
	withoutPassage := *got
	withoutPassage.Passage = ""
	return jsonOf(t, &withoutPassage) == jsonOf(t, want)
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
	d.recordCompaction([]contracts.CompactStats{stageStats("jev", contracts.CompactDone)}, nil)
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
	if len(records) != 1 || !sameEntry(t, records[0].Compaction, recordedCompaction("backend", contracts.CompactShort)) {
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
		note    string
	}{
		{name: "a successful stage", outcome: contracts.CompactDone, kind: eventCompactDone},
		{name: "a failed stage", err: errors.New("boom"), outcome: contracts.CompactFailed, kind: eventCompactFailed, note: `compaction stage "custom": boom`},
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
			want := recordedCompaction("custom", tc.outcome)
			want.Note = tc.note
			if len(records) != 1 || !sameEntry(t, records[0].Compaction, want) {
				t.Fatalf("records = %s, want the custom stage recorded as %s", jsonOf(t, records), tc.outcome)
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
	history := lastCompactions(ledger)
	last, err := history.last, history.readErr
	if err != nil {
		t.Fatalf("lastCompactions: %v", err)
	}
	got, ok := last["jev"]
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
	history := lastCompactions(ledger)
	last, err := history.last, history.readErr
	got, ok := last["jev"]
	if !ok || got.Outcome != contracts.CompactDone {
		t.Fatalf("last jev run = %+v, %t, want the run of the readable day", got, ok)
	}
	if !errors.Is(err, usage.ErrLedgerTruncated) {
		t.Fatalf("error = %v, want ErrLedgerTruncated for the skipped day", err)
	}
}

func TestLastCompactionsKeepsTheRunOfADayBesideAHardReadFailure(t *testing.T) {
	now := time.Now().UTC()
	dir := t.TempDir()
	ledger := usage.Ledger{Dir: dir, Now: time.Now}
	if err := ledger.Append([]usage.Record{
		{Time: now, Session: "s1", Compaction: &usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone}},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	unreadable := filepath.Join(dir, now.AddDate(0, 0, -1).Format(time.DateOnly)+".jsonl")
	if err := os.WriteFile(unreadable, []byte(strings.Repeat("x", 64<<10+1)+"\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	history := lastCompactions(ledger)
	last, err := history.last, history.readErr
	if err == nil || errors.Is(err, usage.ErrLedgerTruncated) {
		t.Fatalf("error = %v, want a hard read failure, not a skipped day", err)
	}
	got, ok := last["jev"]
	if !ok || got.Outcome != contracts.CompactDone {
		t.Fatalf("last jev run = %+v, %t, want the run of the day read after the unreadable one", got, ok)
	}
	probes := compactionProbes([]string{"jev", "autre"}, history, func(string) *health.CompactionKey { return nil })
	kept := probes[0].Check.Facts
	if !kept.CompactionPartial || kept.CompactionUnknown || kept.Compaction == nil {
		t.Fatalf("facts of jev = %+v, want partial with its run kept, not unknown", kept)
	}
	absent := probes[1].Check.Facts
	if !absent.CompactionPartial || !absent.CompactionUnknown || absent.Compaction != nil {
		t.Fatalf("facts of autre = %+v, want partial and unknown, no run claimed", absent)
	}
}

func TestCompactionProbesReportTheKeyTheLastReplyAndTheLastError(t *testing.T) {
	now := time.Now().UTC()
	ledger := usage.Ledger{Dir: t.TempDir(), Now: time.Now}
	if err := ledger.Append([]usage.Record{
		{Time: now.Add(-3 * time.Hour), Session: "s1", Compaction: &usage.Compaction{Stage: "jev", Outcome: contracts.CompactDone}},
		{Time: now.Add(-time.Hour), Session: "s1", Compaction: &usage.Compaction{Stage: "jev", Outcome: contracts.CompactFailed, Note: "boom"}},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	absent := func(string) *health.CompactionKey { return &health.CompactionKey{Env: "TYPESAFE_API_KEY"} }
	probes := compactionProbes([]string{"jev"}, lastCompactions(ledger), absent)
	facts := probes[0].Check.Facts
	if facts.CompactionKey == nil || facts.CompactionKey.Present || facts.CompactionKey.Env != "TYPESAFE_API_KEY" {
		t.Fatalf("key = %+v, want the variable named and absent", facts.CompactionKey)
	}
	if facts.CompactionSucceeded == nil || facts.CompactionSucceeded.Outcome != contracts.CompactDone {
		t.Fatalf("succeeded = %+v, want the done run", facts.CompactionSucceeded)
	}
	if facts.CompactionFailed == nil || facts.CompactionFailed.Note != "boom" || !facts.CompactionFailed.At.Equal(now.Add(-time.Hour)) {
		t.Fatalf("failed = %+v, want the failed run with its note", facts.CompactionFailed)
	}
}

func TestRequiredEnvNamesTheFirstRequiredVariable(t *testing.T) {
	cases := []struct {
		name     string
		settings []contracts.Setting
		want     string
		ok       bool
	}{
		{name: "no setting"},
		{name: "only optional settings", settings: []contracts.Setting{{Key: "pays", Env: "PAYS"}}},
		{
			name:     "the required variable",
			settings: []contracts.Setting{{Key: "pays", Env: "PAYS"}, {Key: "cle", Env: "TYPESAFE_API_KEY", Required: true}},
			want:     "TYPESAFE_API_KEY",
			ok:       true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := requiredEnv(tc.settings)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("requiredEnv = %q, %t, want %q, %t", got, ok, tc.want, tc.ok)
			}
		})
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
	history := lastCompactions(ledger)
	last, err := history.last, history.readErr
	if err != nil {
		t.Fatalf("lastCompactions: %v", err)
	}
	if got, ok := last["jev"]; ok {
		t.Fatalf("last jev run = %+v, want none: the run is older than the probe window", got)
	}
}
