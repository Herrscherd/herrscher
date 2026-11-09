package usage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const claudeTurn = `{"type":"assistant","timestamp":"2026-11-08T10:15:00Z","sessionId":"s1","requestId":"r1","message":{"id":"m1","model":"claude-opus-4-1","usage":{"input_tokens":10,"cache_read_input_tokens":100,"cache_creation_input_tokens":20,"output_tokens":5,"iterations":[{"type":"advisor_message","model":"claude-haiku-4-5","input_tokens":3,"output_tokens":2}]}}}`

const (
	codexMeta    = `{"type":"session_meta","timestamp":"2026-11-08T10:00:00Z","payload":{"id":"c1"}}`
	codexFork    = `{"type":"session_meta","timestamp":"2026-11-08T10:00:00Z","payload":{"id":"c2","forked_from_id":"c1"}}`
	codexContext = `{"type":"turn_context","timestamp":"2026-11-08T10:00:00Z","payload":{"model":"gpt-5"}}`
)

func collect(read func([]byte, func(tokenEvent)), lines ...string) []tokenEvent {
	var events []tokenEvent
	for _, line := range lines {
		read([]byte(line), func(event tokenEvent) { events = append(events, event) })
	}
	return events
}

func codexCount(at string, last, total int64) string {
	return `{"type":"event_msg","timestamp":"` + at + `","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":` +
		strconv.FormatInt(last, 10) + `,"cached_input_tokens":1,"output_tokens":1},"total_token_usage":{"input_tokens":` +
		strconv.FormatInt(total, 10) + `,"output_tokens":1}}}}`
}

func TestClaudeReaderCountsATurnOnceWithItsAdvisor(t *testing.T) {
	reader := claudeReader{seen: make(map[uint64]struct{})}
	events := collect(reader.read, claudeTurn, claudeTurn)
	if len(events) != 2 {
		t.Fatalf("events = %d, want the turn and its advisor once", len(events))
	}
	if events[0].model != "claude-opus-4-1" || events[0].in != 10 || events[0].cacheRead != 100 || events[0].cacheCreate != 20 {
		t.Fatalf("turn = %+v", events[0])
	}
	if events[1].model != "claude-haiku-4-5" || events[1].in != 3 || events[1].out != 2 {
		t.Fatalf("advisor = %+v", events[1])
	}
}

func TestClaudeReaderSkipsSyntheticAndUserLines(t *testing.T) {
	reader := claudeReader{seen: make(map[uint64]struct{})}
	synthetic := strings.NewReplacer("claude-opus-4-1", "<synthetic>", `"advisor_message"`, `"message"`).Replace(claudeTurn)
	if events := collect(reader.read, synthetic, `{"type":"user","message":{"usage":{}}}`); len(events) != 0 {
		t.Fatalf("events = %+v, want none", events)
	}
}

func TestCodexReaderSkipsRepeatedTotals(t *testing.T) {
	events := collect((&codexReader{}).read, codexMeta, codexContext,
		codexCount("2026-11-08T10:01:00Z", 10, 10),
		codexCount("2026-11-08T10:01:01Z", 10, 10),
		codexCount("2026-11-08T10:02:00Z", 7, 17))
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].session != "c1" || events[0].model != "gpt-5" || events[0].in != 9 || events[0].cacheRead != 1 {
		t.Fatalf("first = %+v", events[0])
	}
}

func TestCodexReaderSkipsTheTurnsAForkCopies(t *testing.T) {
	events := collect((&codexReader{}).read, codexFork, codexContext,
		codexCount("2026-11-08T10:00:00Z", 10, 10),
		codexCount("2026-11-08T10:00:00.5Z", 5, 15),
		codexCount("2026-11-08T10:05:00Z", 4, 19))
	if len(events) != 1 || events[0].in != 3 {
		t.Fatalf("events = %+v, want only the turn after the copy", events)
	}
}

func TestScanTranscriptLeavesAPartialLineForTheNextScan(t *testing.T) {
	var lines []string
	consumed, err := scanTranscript(context.Background(), strings.NewReader("one\ntwo\nthr"), func(line []byte) { lines = append(lines, string(line)) })
	if err != nil {
		t.Fatal(err)
	}
	if consumed != int64(len("one\ntwo\n")) || len(lines) != 2 || lines[1] != "two\n" {
		t.Fatalf("consumed = %d, lines = %q", consumed, lines)
	}
}

func TestScanTranscriptJoinsALineLongerThanTheBuffer(t *testing.T) {
	long := strings.Repeat("x", transcriptBuffer*2) + "\n"
	var visited int
	consumed, err := scanTranscript(context.Background(), strings.NewReader(long+"end\n"), func(line []byte) { visited += len(line) })
	if err != nil {
		t.Fatal(err)
	}
	if consumed != int64(len(long)+len("end\n")) || visited != len(long)+len("end\n") {
		t.Fatalf("consumed = %d, visited = %d", consumed, visited)
	}
}

func TestTallyAddsTheEventsOfOneHourSessionAndModel(t *testing.T) {
	totals := newTally("claude", "pro", nil)
	at := time.Date(2026, 11, 8, 10, 5, 0, 0, time.UTC)
	totals.add(tokenEvent{at: at, session: "s", model: "m", in: 1})
	totals.add(tokenEvent{at: at.Add(20 * time.Minute), session: "s", model: "m", out: 2})
	totals.add(tokenEvent{at: at, session: "s", model: "m"})
	totals.add(tokenEvent{at: at.Add(time.Hour), session: "s", model: "m", in: 4})
	if len(totals.records) != 2 || totals.records[0].In != 1 || totals.records[0].Out != 2 || totals.records[0].Account != "pro" {
		t.Fatalf("records = %+v", totals.records)
	}
}

func writeTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendTranscript(t *testing.T, path, line string) {
	t.Helper()
	handle, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := handle.WriteString(line + "\n")
	if err := errors.Join(writeErr, handle.Close()); err != nil {
		t.Fatal(err)
	}
}

func stampTranscript(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestTranscriptIndexReadsOnlyTheNewLinesAndSkipsALinkedHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "claude")
	transcript := filepath.Join(home, claudeTranscripts, "proj", "a"+fileExt)
	written := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	writeTranscript(t, transcript, claudeTurn)
	stampTranscript(t, transcript, written)
	if err := os.Symlink(home, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return written.Add(12 * time.Hour) }
	sources := []TranscriptSource{
		{Vendor: contracts.VendorClaude, Account: "pro", Home: home},
		{Vendor: contracts.VendorClaude, Account: "perso", Home: filepath.Join(root, "linked")},
	}
	index := &TranscriptIndex{Path: filepath.Join(root, "index.json"), Now: now}
	if err := index.Scan(context.Background(), sources); err != nil {
		t.Fatal(err)
	}
	appendTranscript(t, transcript, strings.NewReplacer(`"m1"`, `"m2"`, `"r1"`, `"r2"`).Replace(claudeTurn))
	stampTranscript(t, transcript, written.Add(time.Minute))
	reopened := &TranscriptIndex{Path: index.Path, Now: now}
	if err := reopened.Scan(context.Background(), sources); err != nil {
		t.Fatal(err)
	}
	var in int64
	view := reopened.View(written.Add(-24 * time.Hour))
	for _, record := range view.Records {
		in += record.In
		if record.Account != "pro" {
			t.Fatalf("record of %q, want the first source only", record.Account)
		}
	}
	if in != 26 {
		t.Fatalf("input tokens = %d, want two turns and two advisors", in)
	}
	if !view.Covered[AccountKey{Vendor: contracts.VendorClaude, Account: "pro"}] || view.Covered[AccountKey{Vendor: contracts.VendorClaude, Account: "perso"}] || !view.Scanned {
		t.Fatalf("covered = %v, scanned = %v", view.Covered, view.Scanned)
	}
}

func TestTranscriptIndexSkipsAnUnreadableFileAndDropsAnUncoveredAccount(t *testing.T) {
	root := t.TempDir()
	written := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return written.Add(time.Hour) }
	homes := []string{filepath.Join(root, "a"), filepath.Join(root, "b"), filepath.Join(root, "c")}
	shut := filepath.Join(homes[2], claudeTranscripts)
	if err := os.MkdirAll(shut, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shut, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(shut, 0o700); err != nil {
			t.Error(err)
		}
	})
	locked := filepath.Join(homes[0], claudeTranscripts, "p", "locked"+fileExt)
	writeTranscript(t, locked, claudeTurn)
	writeTranscript(t, filepath.Join(homes[0], claudeTranscripts, "p", "open"+fileExt), strings.NewReplacer(`"m1"`, `"m2"`, `"r1"`, `"r2"`).Replace(claudeTurn))
	writeTranscript(t, filepath.Join(homes[1], claudeTranscripts, "p", "b"+fileExt), strings.NewReplacer(`"m1"`, `"m3"`, `"r1"`, `"r3"`).Replace(claudeTurn))
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	sources := []TranscriptSource{
		{Vendor: contracts.VendorClaude, Account: "a", Home: homes[0]},
		{Vendor: contracts.VendorClaude, Account: "b", Home: homes[1]},
		{Vendor: contracts.VendorClaude, Account: "c", Home: homes[2]},
	}
	index := &TranscriptIndex{Path: filepath.Join(root, "index.json"), Now: now}
	if err := index.Scan(context.Background(), sources); err != nil {
		t.Fatal(err)
	}
	if view := index.View(written.Add(-24 * time.Hour)); len(view.Records) != 4 || view.Unreadable != 2 || !view.Scanned {
		t.Fatalf("records = %d, unreadable = %d, scanned = %v", len(view.Records), view.Unreadable, view.Scanned)
	}
	if err := index.Scan(context.Background(), sources[1:2]); err != nil {
		t.Fatal(err)
	}
	for _, record := range index.View(written.Add(-24 * time.Hour)).Records {
		if record.Account != "b" {
			t.Fatalf("record of %q counted once no source covers it", record.Account)
		}
	}
}

func TestTranscriptIndexReadsAgainATruncatedTranscript(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "claude")
	transcript := filepath.Join(home, claudeTranscripts, "p", "a"+fileExt)
	written := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return written.Add(time.Hour) }
	turn := func(id string) string {
		return strings.NewReplacer(`"m1"`, `"m`+id+`"`, `"r1"`, `"r`+id+`"`).Replace(claudeTurn)
	}
	writeTranscript(t, transcript, claudeTurn, turn("2"), turn("3"))
	stampTranscript(t, transcript, written)
	sources := []TranscriptSource{{Vendor: contracts.VendorClaude, Account: "pro", Home: home}}
	index := &TranscriptIndex{Path: filepath.Join(root, "index.json"), Now: now}
	if err := index.Scan(context.Background(), sources); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, transcript, claudeTurn, turn("9"))
	stampTranscript(t, transcript, written.Add(time.Minute))
	if err := index.Scan(context.Background(), sources); err != nil {
		t.Fatal(err)
	}
	var in int64
	for _, record := range index.View(written.Add(-24 * time.Hour)).Records {
		in += record.In
	}
	if in != 52 {
		t.Fatalf("input tokens = %d, want the three turns kept once and the new one read", in)
	}
}
