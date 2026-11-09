package usage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testNow() time.Time {
	return time.Date(2026, time.November, 8, 12, 0, 0, 0, time.UTC)
}

func clock(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

func TestLedgerAppendWritesOneFilePerDay(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "usage")
	ledger := Ledger{Dir: dir, Now: clock(testNow())}
	records := []Record{
		{Time: testNow(), Session: "a", Model: "opus", In: 1},
		{Time: testNow().Add(-24 * time.Hour), Session: "b", Model: "haiku", Out: 2},
		{Time: testNow(), Session: "a", Model: "haiku", Cost: 0.5},
	}
	if err := ledger.Append(records); err != nil {
		t.Fatalf("Append: %v", err)
	}
	for _, name := range []string{"2026-11-08.jsonl", "2026-11-07.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing daily file %s: %v", name, err)
		}
	}
	got, err := ledger.Records(Range7d)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(got) != len(records) {
		t.Fatalf("Records returned %d, want %d", len(got), len(records))
	}
}

func TestLedgerAppendEmptyWritesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "usage")
	if err := (Ledger{Dir: dir, Now: clock(testNow())}).Append(nil); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory created for no records: %v", err)
	}
}

func TestLedgerPruneRemovesOnlyFilesPastRetention(t *testing.T) {
	dir := t.TempDir()
	old := testNow().Add(-Retention - 48*time.Hour)
	kept := testNow().Add(-Retention + 48*time.Hour)
	for _, at := range []time.Time{old, kept} {
		if err := os.WriteFile(filepath.Join(dir, fileName(at)), []byte("{}\n"), fileMode); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), fileMode); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := (Ledger{Dir: dir, Now: clock(testNow())}).Prune(); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName(old))); !os.IsNotExist(err) {
		t.Errorf("old file kept: %v", err)
	}
	for _, name := range []string{fileName(kept), "notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s removed: %v", name, err)
		}
	}
}

func TestLedgerRecordsMissingDirIsEmpty(t *testing.T) {
	ledger := Ledger{Dir: filepath.Join(t.TempDir(), "absent"), Now: clock(testNow())}
	got, err := ledger.Records(Range30d)
	if err != nil || len(got) != 0 {
		t.Fatalf("Records = %v, %v; want none and no error", got, err)
	}
}

func TestLedgerRecordsSkipsBadLinesAndOutsideWindow(t *testing.T) {
	dir := t.TempDir()
	body := `{"time":"2026-11-08T10:00:00Z","vendor":"claude","model":"opus","in":3}
not json
{"time":"2026-11-08T11:00:00Z","vendor":"codex","model":"gpt","in":4}
{"time":"2026-11-07T08:00:00Z","vendor":"claude","model":"opus","in":5}
`
	if err := os.WriteFile(filepath.Join(dir, "2026-11-08.jsonl"), []byte(body), fileMode); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ledger := Ledger{Dir: dir, Now: clock(testNow())}
	got, err := ledger.Records(Range24h)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Records returned %d, want the 2 decodable lines of the window, the day before dropped", len(got))
	}
}

func TestLedgerAppendPrunesFilesPastRetention(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "2026-01-01.jsonl")
	if err := os.WriteFile(old, []byte(`{"in":1}`+"\n"), fileMode); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ledger := Ledger{Dir: dir, Now: clock(testNow())}
	if err := ledger.Append([]Record{{Time: testNow(), Model: "opus", In: 1}}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := os.Stat(old); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file past retention still there: %v", err)
	}
}

func TestLedgerRecordsKeepsTheReadableDaysBesideATruncatedOne(t *testing.T) {
	dir := t.TempDir()
	ledger := Ledger{Dir: dir, Now: clock(testNow())}
	if err := ledger.Append([]Record{{Time: testNow().Add(-time.Hour), Session: "s1", Model: "opus"}}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	file, err := os.Create(filepath.Join(dir, "2026-11-07.jsonl"))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err = file.Truncate(maxLedgerFile + 1); err != nil {
		t.Fatalf("grow: %v", err)
	}
	if err = file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	got, err := ledger.Records(Range24h)
	if !errors.Is(err, ErrLedgerTruncated) {
		t.Fatalf("Records error = %v, want ErrLedgerTruncated", err)
	}
	if len(got) != 1 || got[0].Model != "opus" {
		t.Fatalf("Records = %+v, want the one record of the readable day", got)
	}
}
