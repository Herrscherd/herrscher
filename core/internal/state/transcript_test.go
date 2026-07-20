package state

import (
	"path/filepath"
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
