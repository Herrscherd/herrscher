package manager

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func seedTranscript(t *testing.T, partDir string, entries []state.TranscriptEntry) {
	t.Helper()
	path := state.TranscriptPath(partDir, "alpha")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	enc := json.NewEncoder(f)
	for idx := range entries {
		if err := enc.Encode(entries[idx]); err != nil {
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
	seedTranscript(t, dir, []state.TranscriptEntry{
		{Ts: "t1", Role: "user", Text: "salut"},
		{Ts: "t2", Role: "assistant", Text: "bonjour", Cost: 0.02},
	})

	out, err := cli.Plain(h.sessionLogRun(context.Background(), contractsInputJSON("alpha")))
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

func TestSessionLogRun_ShowsTheNoticeOfAReplyWithoutText(t *testing.T) {
	dir := t.TempDir()
	h := &Handler{partDir: dir}
	seedTranscript(t, dir, []state.TranscriptEntry{
		{Ts: "t1", Role: "assistant", Text: "", Displayed: "fichier refusé: big.bin"},
	})

	out, err := cli.Plain(h.sessionLogRun(context.Background(), contracts.Input{Args: map[string]string{"name": "alpha"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "assistant fichier refusé: big.bin\n"; out != want {
		t.Fatalf("session view = %q, want %q: the human saw the notice, so the view shows it", out, want)
	}
}

func TestSessionLogRun_OmitsAnEntryWithNothingShown(t *testing.T) {
	dir := t.TempDir()
	h := &Handler{partDir: dir}
	seedTranscript(t, dir, []state.TranscriptEntry{
		{Ts: "t1", Role: "user", Text: "salut"},
		{Ts: "t2", Role: "assistant", Text: ""},
	})

	out, err := cli.Plain(h.sessionLogRun(context.Background(), contracts.Input{Args: map[string]string{"name": "alpha"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "user salut\n"; out != want {
		t.Fatalf("session view = %q, want %q: an entry with nothing shown must not appear", out, want)
	}
}

func TestSessionLogRun_JSONKeepsTextAndDisplayedOfANoticeOnlyReply(t *testing.T) {
	dir := t.TempDir()
	h := &Handler{partDir: dir}
	seedTranscript(t, dir, []state.TranscriptEntry{
		{Ts: "t1", Role: "assistant", Text: "", Displayed: "fichier refusé: big.bin"},
	})

	out, err := cli.Plain(h.sessionLogRun(context.Background(), contractsInputJSON("alpha")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("output is not a JSON array: %v (%q)", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 entry, got %d", len(rows))
	}
	var text, displayed string
	if err := json.Unmarshal(rows[0]["text"], &text); err != nil {
		t.Fatalf("text key missing or not a string: %v (%+v)", err, rows[0])
	}
	if err := json.Unmarshal(rows[0]["displayed"], &displayed); err != nil {
		t.Fatalf("displayed key missing or not a string: %v (%+v)", err, rows[0])
	}
	if text != "" || displayed != "fichier refusé: big.bin" {
		t.Fatalf("text = %q, displayed = %q, want an empty text and the notice as displayed", text, displayed)
	}
}

func TestSessionLogRun_UnknownSessionIsEmptyArray(t *testing.T) {
	h := &Handler{partDir: t.TempDir()}
	out, err := cli.Plain(h.sessionLogRun(context.Background(), contractsInputJSON("ghost")))
	if err != nil {
		t.Fatalf("unknown session must not error, got %v", err)
	}
	if out != "[]" {
		t.Fatalf("want [] for unknown session, got %q", out)
	}
}

func TestSessionLogRun_TraversalNameIsEmpty(t *testing.T) {
	h := &Handler{partDir: t.TempDir()}
	out, err := cli.Plain(h.sessionLogRun(context.Background(), contractsInputJSON("../../../etc/passwd")))
	if err != nil {
		t.Fatalf("crafted name must not error, got %v", err)
	}
	if out != "[]" {
		t.Fatalf("want [] for traversal name, got %q", out)
	}
}
