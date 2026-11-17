package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscriptEntryPersistsTurnID(t *testing.T) {
	path := TranscriptPath(t.TempDir(), "s1")
	if err := AppendTranscript(path, TranscriptEntry{Ts: "t", Role: "assistant", Text: "ok", TurnID: "turn-7"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"turn":"turn-7"`) {
		t.Fatalf("turn key not written: %s", raw)
	}
	got := ReadTranscript(path, -1)
	if len(got) != 1 || got[0].TurnID != "turn-7" || got[0].Text != "ok" {
		t.Fatalf("TurnID not read back: %+v", got)
	}
}

func TestTranscriptEntryOffTurnOmitsTurnKey(t *testing.T) {
	path := TranscriptPath(t.TempDir(), "s1")
	if err := AppendTranscript(path, TranscriptEntry{Ts: "t", Role: "notice", Text: "hors tour"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"turn"`) {
		t.Fatalf("empty TurnID written as a key: %s", raw)
	}
}

func TestTranscriptWithoutTurnKeyReadsCleanly(t *testing.T) {
	path := TranscriptPath(t.TempDir(), "s1")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"ts":"t","role":"assistant","text":"ok"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ReadTranscript(path, -1)
	if len(got) != 1 || got[0].Role != "assistant" || got[0].Text != "ok" || got[0].TurnID != "" {
		t.Fatalf("old transcript read as %+v", got)
	}
}
