package state

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/atomicfile"
)

var errInjectedSave = errors.New("injected save failure")

func failEveryWrite(string, []byte, os.FileMode) error {
	return errInjectedSave
}

func failRestoredTokenWithoutMark(name string, data []byte, perm os.FileMode) error {
	if bytes.Contains(data, []byte(`"resumeToken": "new"`)) && !bytes.Contains(data, []byte(`"interruptedTurn"`)) {
		return errInjectedSave
	}
	return atomicfile.Write(name, data, perm)
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
	var writes int
	s.write = func(name string, data []byte, perm os.FileMode) error {
		writes++
		return atomicfile.Write(name, data, perm)
	}

	must(t, s.SetResumeTokenAndClearInterruptedTurn("main", "new"))

	if writes != 1 {
		t.Fatalf("saves = %d, want exactly 1 for the token and the cleared mark", writes)
	}
	reloaded, err := LoadState(path)
	must(t, err)
	if got, _ := reloaded.FindSession("main"); got.ResumeToken != "new" || got.InterruptedTurn != "" {
		t.Fatalf("file = token %q, mark %q, want new and no mark", got.ResumeToken, got.InterruptedTurn)
	}
}

func TestSetResumeTokenAndClearInterruptedTurnConcurrentMemoryMatchesDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	must(t, s.AddSession(Session{Name: "main", ResumeToken: "old", InterruptedTurn: "turn-1"}))
	var calls atomic.Int64
	s.write = func(name string, data []byte, perm os.FileMode) error {
		if s.mu.TryLock() {
			t.Error("save ran without the state lock")
			s.mu.Unlock()
		}
		if calls.Add(1)%3 == 0 {
			return errInjectedSave
		}
		return atomicfile.Write(name, data, perm)
	}

	var failures atomic.Int64
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.SetResumeTokenAndClearInterruptedTurn("main", fmt.Sprintf("tok-%d", i))
			switch {
			case errors.Is(err, errInjectedSave):
				failures.Add(1)
			case err != nil:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if failures.Load() == 0 {
		t.Fatal("no save failed, so the rollback path was not exercised")
	}

	reloaded, err := LoadState(path)
	must(t, err)
	mem, _ := s.FindSession("main")
	disk, _ := reloaded.FindSession("main")
	if mem.ResumeToken != disk.ResumeToken || mem.InterruptedTurn != disk.InterruptedTurn {
		t.Fatalf("memory = token %q, mark %q; disk = token %q, mark %q: want equal", mem.ResumeToken, mem.InterruptedTurn, disk.ResumeToken, disk.InterruptedTurn)
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
