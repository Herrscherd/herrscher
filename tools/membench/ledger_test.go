package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/usage"
)

func TestReadTallyKeepsTheReadableDaysWhenOneIsUnreadable(t *testing.T) {
	stateDir := t.TempDir()
	dir := filepath.Join(stateDir, usageSubdir)
	now := time.Now()
	unreadable := now.Add(-24*time.Hour).UTC().Format(time.DateOnly) + ".jsonl"
	if err := os.MkdirAll(filepath.Join(dir, unreadable), 0o700); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(usage.Record{Time: now, In: 10, Out: 5, CacheRead: 1, CacheCreate: 3, Cost: 0.25})
	if err != nil {
		t.Fatal(err)
	}
	readable := now.UTC().Format(time.DateOnly) + ".jsonl"
	if err = os.WriteFile(filepath.Join(dir, readable), append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	total, err := readTally(stateDir)
	if err == nil {
		t.Fatal("want the unreadable day reported as an error")
	}
	if total.turns != 1 || total.input != 14 || total.tokens != 19 || total.usd != 0.25 {
		t.Errorf("tally %+v, want the readable record counted, cached prompt tokens in the input", total)
	}
}
