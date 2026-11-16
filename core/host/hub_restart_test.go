package host

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestRestartWithResumeKeepsOldTokenAndMarkOnDiskWhenPersistFails(t *testing.T) {
	name := "restore-" + newTurnID()
	path := filepath.Join(t.TempDir(), "state.json")
	st := state.NewState(path)
	if err := st.AddSession(state.Session{Name: name, ResumeToken: "old", InterruptedTurn: "turn-1", Incarnation: "incarnation"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	partDir := t.TempDir()
	h := newHub(st, nil, nil, partDir, nil, nil, newApprovalTable(io.Discard), newSessionTable(), withWarnings(io.Discard))
	h.runtime = isolatedRuntimeDir(t)
	h.startBridge = func(state.Session, time.Duration) error { return nil }

	if err := h.RestartWithResume(name, "new"); err == nil {
		t.Fatal("RestartWithResume succeeded although the state could not be saved")
	}
	onDisk, err := state.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	disk, _ := onDisk.FindSession(name)
	if disk.ResumeToken != "old" || disk.InterruptedTurn != "turn-1" {
		t.Fatalf("state file after a failed restore = token %q, mark %q, want old and turn-1", disk.ResumeToken, disk.InterruptedTurn)
	}
	memory, _ := st.FindSession(name)
	if memory.ResumeToken != "old" || memory.InterruptedTurn != "turn-1" {
		t.Fatalf("memory after a failed restore = token %q, mark %q, want old and turn-1", memory.ResumeToken, memory.InterruptedTurn)
	}
	if entries := state.ReadTranscript(state.TranscriptPath(partDir, name), 10); len(entries) != 0 {
		t.Fatalf("transcript = %+v after a failed restore, want no notice", entries)
	}
}
