package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscriptAppendReadCap(t *testing.T) {
	dir := t.TempDir()
	p := TranscriptPath(dir, "sess")
	for i, role := range []string{"user", "assistant", "user", "assistant"} {
		if err := AppendTranscript(p, TranscriptEntry{Ts: "t", Role: role, Text: string(rune('a' + i))}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	all := ReadTranscript(p, 0)
	if len(all) != 4 {
		t.Fatalf("want 4 entries, got %d", len(all))
	}
	if all[0].Role != "user" || all[3].Role != "assistant" {
		t.Fatalf("order wrong: %+v", all)
	}
	last2 := ReadTranscript(p, 2)
	if len(last2) != 2 || last2[0].Text != "c" || last2[1].Text != "d" {
		t.Fatalf("cap wrong: %+v", last2)
	}
	if got := TranscriptPath(dir, "sess"); got != filepath.Join(dir, "transcripts", "sess.jsonl") {
		t.Fatalf("path: %s", got)
	}
}

func TestReadTranscriptLast(t *testing.T) {
	dir := t.TempDir()
	p := TranscriptPath(dir, "sess")
	if got := ReadTranscriptLast(p); got != "" {
		t.Fatalf("missing file must yield empty ts, got %q", got)
	}
	for _, e := range []TranscriptEntry{
		{Ts: "t1", Role: "user", Text: "hi"},
		{Ts: "t2", Role: "assistant", Text: "yo"},
		{Ts: "t3", Role: "user", Text: "again"},
	} {
		if err := AppendTranscript(p, e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if got := ReadTranscriptLast(p); got != "t3" {
		t.Fatalf("want last ts t3, got %q", got)
	}

	// A transcript larger than the tail window must still find the newest entry
	// (the tail read starts mid-file, and the scan-from-end reaches the last line).
	big := strings.Repeat("x", 4096)
	for i := 0; i < 40; i++ {
		if err := AppendTranscript(p, TranscriptEntry{Ts: "t3", Role: "assistant", Text: big}); err != nil {
			t.Fatalf("append big: %v", err)
		}
	}
	if err := AppendTranscript(p, TranscriptEntry{Ts: "tLAST", Role: "user", Text: "final"}); err != nil {
		t.Fatalf("append final: %v", err)
	}
	if got := ReadTranscriptLast(p); got != "tLAST" {
		t.Fatalf("want last ts tLAST across a >64KB file, got %q", got)
	}
}

// An entry far larger than any fixed scanner-token cap must round-trip intact,
// not be silently dropped: bufio.Reader grows the line as needed.
func TestReadTranscriptHandlesHugeLine(t *testing.T) {
	dir := t.TempDir()
	p := TranscriptPath(dir, "sess")
	huge := strings.Repeat("z", 2<<20) // 2 MB, past the old 1 MB scanner cap
	for _, e := range []TranscriptEntry{
		{Ts: "t1", Role: "user", Text: "hi"},
		{Ts: "t2", Role: "assistant", Text: huge},
	} {
		if err := AppendTranscript(p, e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	all := ReadTranscript(p, 0)
	if len(all) != 2 {
		t.Fatalf("want 2 entries, got %d", len(all))
	}
	if len(all[1].Text) != len(huge) {
		t.Fatalf("huge entry truncated: want %d bytes, got %d", len(huge), len(all[1].Text))
	}
}

// A long-lived session must not grow its transcript without bound: once it crosses
// transcriptMaxBytes, compaction rewrites it to its newest entries. The very last
// entry always survives; older ones are dropped.
func TestTranscriptCompactionBoundsGrowth(t *testing.T) {
	dir := t.TempDir()
	p := TranscriptPath(dir, "sess")
	body := strings.Repeat("m", 5000)
	const n = 1000 // ~5 MB raw, crossing the 4 MB trigger
	for i := 0; i < n; i++ {
		if err := AppendTranscript(p, TranscriptEntry{Ts: fmt.Sprintf("t%04d", i), Role: "user", Text: body}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Size() > transcriptMaxBytes {
		t.Fatalf("transcript not bounded: %d bytes > %d", fi.Size(), transcriptMaxBytes)
	}
	all := ReadTranscript(p, 0)
	if len(all) == 0 {
		t.Fatal("compaction dropped every entry")
	}
	if len(all) >= n {
		t.Fatalf("compaction retained all %d entries, expected fewer", len(all))
	}
	if last := all[len(all)-1]; last.Ts != fmt.Sprintf("t%04d", n-1) {
		t.Fatalf("newest entry lost: got %q", last.Ts)
	}
}

// When the newest entry is larger than the tail window, ReadTranscriptLast must
// fall back to a full read rather than return no timestamp.
func TestReadTranscriptLastFallsBackPastTailWindow(t *testing.T) {
	dir := t.TempDir()
	p := TranscriptPath(dir, "sess")
	if err := AppendTranscript(p, TranscriptEntry{Ts: "t1", Role: "user", Text: "hi"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	// Final entry's own JSON exceeds the 64 KB tail window, so the tail scan finds
	// only a partial (unparseable) line and the fallback must kick in.
	if err := AppendTranscript(p, TranscriptEntry{Ts: "tHUGE", Role: "assistant", Text: strings.Repeat("y", 128*1024)}); err != nil {
		t.Fatalf("append huge: %v", err)
	}
	if got := ReadTranscriptLast(p); got != "tHUGE" {
		t.Fatalf("want tHUGE via fallback, got %q", got)
	}
}

func TestTranscriptReadMissingAndRemove(t *testing.T) {
	dir := t.TempDir()
	p := TranscriptPath(dir, "gone")
	if got := ReadTranscript(p, 0); got != nil {
		t.Fatalf("missing file should read nil, got %v", got)
	}
	if err := RemoveTranscript(p); err != nil {
		t.Fatalf("remove missing should be nil, got %v", err)
	}
}
