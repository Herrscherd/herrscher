package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
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
	if err := Write(path, []byte("new\n"), 0o600); err == nil {
		t.Fatal("Write onto a directory returned no error")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "state" {
		t.Fatalf("entries = %v, %v; want only the blocking directory", entries, err)
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
