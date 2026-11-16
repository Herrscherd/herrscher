package state

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

var errInjectedSave = errors.New("injected save failure")

func failEveryWrite(string, []byte, os.FileMode) error {
	return errInjectedSave
}

func failRestoredTokenWithoutMark(name string, data []byte, perm os.FileMode) error {
	if bytes.Contains(data, []byte(`"resumeToken": "new"`)) && !bytes.Contains(data, []byte(`"interruptedTurn"`)) {
		return errInjectedSave
	}
	return os.WriteFile(name, data, perm)
}

func TestSetResumeTokenRollsBackWhenSaveFails(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	must(t, s.AddSession(Session{Name: "main", ResumeToken: "old"}))
	s.write = failEveryWrite

	if err := s.SetResumeToken("main", "new"); !errors.Is(err, errInjectedSave) {
		t.Fatalf("SetResumeToken error = %v, want the save failure", err)
	}
	if got, _ := s.FindSession("main"); got.ResumeToken != "old" {
		t.Fatalf("memory token = %q after a failed save, want old", got.ResumeToken)
	}
}

func TestSetResumeTokenAndClearInterruptedTurnStoresBothInOneSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	must(t, s.AddSession(Session{Name: "main", ResumeToken: "old", InterruptedTurn: "turn-1"}))

	must(t, s.SetResumeTokenAndClearInterruptedTurn("main", "new"))

	reloaded, err := LoadState(path)
	must(t, err)
	if got, _ := reloaded.FindSession("main"); got.ResumeToken != "new" || got.InterruptedTurn != "" {
		t.Fatalf("file = token %q, mark %q, want new and no mark", got.ResumeToken, got.InterruptedTurn)
	}
}

func TestRestoreFailedSaveKeepsOldTokenAndMarkOnDiskAndInMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	must(t, s.AddSession(Session{Name: "main", ResumeToken: "old", InterruptedTurn: "turn-1"}))
	s.write = failRestoredTokenWithoutMark

	if err := s.SetResumeTokenAndClearInterruptedTurn("main", "new"); !errors.Is(err, errInjectedSave) {
		t.Fatalf("restore error = %v, want the save failure", err)
	}
	reloaded, err := LoadState(path)
	must(t, err)
	if got, _ := reloaded.FindSession("main"); got.ResumeToken != "old" || got.InterruptedTurn != "turn-1" {
		t.Fatalf("file after a failed restore = token %q, mark %q, want old and turn-1", got.ResumeToken, got.InterruptedTurn)
	}
	if got, _ := s.FindSession("main"); got.ResumeToken != "old" || got.InterruptedTurn != "turn-1" {
		t.Fatalf("memory after a failed restore = token %q, mark %q, want old and turn-1", got.ResumeToken, got.InterruptedTurn)
	}
}
