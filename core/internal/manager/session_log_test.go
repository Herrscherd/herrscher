package manager

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// seedTranscript writes newline-delimited entries to partDir/transcripts/<name>.jsonl.
func seedTranscript(t *testing.T, partDir, name string, entries []state.TranscriptEntry) {
	t.Helper()
	path := state.TranscriptPath(partDir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatal(err)
		}
	}
}

func contractsInputJSON(name string) contracts.Input {
	return contracts.Input{JSON: true, Args: map[string]string{"name": name}}
}

func TestSessionLogRun_JSONShapeAndOrder(t *testing.T) {
	dir := t.TempDir()
	h := &Handler{partDir: dir}
	seedTranscript(t, dir, "alpha", []state.TranscriptEntry{
		{Ts: "t1", Role: "user", Text: "salut"},
		{Ts: "t2", Role: "assistant", Text: "bonjour", Cost: 0.02},
	})

	out, err := h.sessionLogRun(context.Background(), contractsInputJSON("alpha"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var rows []state.TranscriptEntry
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("output is not a JSON array: %v (%q)", err, out)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 entries, got %d", len(rows))
	}
	if rows[0].Role != "user" || rows[1].Role != "assistant" {
		t.Fatalf("wrong order/roles: %+v", rows)
	}
}

func TestSessionLogRun_UnknownSessionIsEmptyArray(t *testing.T) {
	h := &Handler{partDir: t.TempDir()}
	out, err := h.sessionLogRun(context.Background(), contractsInputJSON("ghost"))
	if err != nil {
		t.Fatalf("unknown session must not error, got %v", err)
	}
	if out != "[]" {
		t.Fatalf("want [] for unknown session, got %q", out)
	}
}

// A crafted path-traversal name must never escape the transcripts dir: it has
// no valid session slug, so the verb returns empty history without ever reading
// a file outside partDir/transcripts.
func TestSessionLogRun_TraversalNameIsEmpty(t *testing.T) {
	h := &Handler{partDir: t.TempDir()}
	out, err := h.sessionLogRun(context.Background(), contractsInputJSON("../../../etc/passwd"))
	if err != nil {
		t.Fatalf("crafted name must not error, got %v", err)
	}
	if out != "[]" {
		t.Fatalf("want [] for traversal name, got %q", out)
	}
}
