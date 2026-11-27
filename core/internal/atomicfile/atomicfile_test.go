package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteReplacesContentAndGivesPerm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := Write(path, []byte("new\n"), 0o640); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "new\n" {
		t.Fatalf("content = %q, %v; want new", raw, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, %v; want 0640", info.Mode().Perm(), err)
	}
}

func TestWriteRenameFailureLeavesNoStagedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("block the target with a directory: %v", err)
	}
	err := Write(path, []byte("new\n"), 0o600)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("Write onto a directory error = %v; want an error naming %q", err, path)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "state" {
		t.Fatalf("entries = %v, %v; want only the blocking directory", entries, err)
	}
}

func TestSweepRemovesStaleStagingFilesOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	stale := filepath.Join(dir, ".state.json.4242517")
	kept := []string{"state.json", "state.json.bak", ".state.json.keep", ".state.json.tmp", ".state.jsonx.42", "other.json.1"}
	for _, name := range append([]string{filepath.Base(stale)}, kept...) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Sweep(path); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale staging file survived the sweep: %v", err)
	}
	for _, name := range kept {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("Sweep removed %s, which is not a staging file of state.json: %v", name, err)
		}
	}
}

func TestSweepOfAMissingDirectoryIsNotAnError(t *testing.T) {
	if err := Sweep(filepath.Join(t.TempDir(), "absent", "state.json")); err != nil {
		t.Fatalf("Sweep of a missing directory: %v", err)
	}
}

func TestConcurrentWritersOfOneFileDoNotClash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	var wg sync.WaitGroup
	errs := make(chan error, 8*20)
	for writer := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				errs <- Write(path, []byte(fmt.Sprintf("writer %d\n", writer)), 0o600)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write failed: %v", err)
		}
	}
}
