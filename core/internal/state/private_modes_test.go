package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateStateFilesAndDirectoriesAreOwnerOnly(t *testing.T) {
	root := t.TempDir()

	journal := filepath.Join(root, "participants", "a.log")
	_, err := AppendParticipant(journal, "u1")
	must(t, err)
	assertMode(t, journal, 0o600)
	assertMode(t, filepath.Dir(journal), 0o750)

	transcript := TranscriptPath(filepath.Join(root, "sessions"), "s1")
	must(t, AppendTranscript(transcript, TranscriptEntry{Ts: "t", Role: "user", Text: "hi"}))
	assertMode(t, transcript, 0o600)
	assertMode(t, filepath.Dir(transcript), 0o750)

	statePath := filepath.Join(root, "state", "state.json")
	must(t, NewState(statePath).Save())
	assertMode(t, statePath, 0o600)
	assertMode(t, filepath.Dir(statePath), 0o750)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}
