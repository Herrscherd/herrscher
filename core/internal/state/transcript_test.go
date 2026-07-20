package state

import (
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
