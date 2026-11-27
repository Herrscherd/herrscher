package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
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
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := Sweep(path); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale staging file survived the sweep: %v", err)
	}
	for _, name := range kept {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("Sweep removed %s, which is not a stale staging file of state.json: %v", name, err)
		}
	}
}

func TestSweepKeepsAFreshStagingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	live := filepath.Join(dir, ".state.json.4242518")
	if err := os.WriteFile(live, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Sweep(path); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("Sweep removed a staging file that a live writer may hold: %v", err)
	}
}

func TestSweepReportsARemovalFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where directory modes do not block removal")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	stale := filepath.Join(dir, ".state.json.4242519")
	if err := os.WriteFile(stale, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Error(err)
		}
	})
	if err := Sweep(path); err == nil {
		t.Fatal("Sweep of a stale file in a read-only directory returned no error")
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

func TestConcurrentSweepNeverFailsAWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	var wg sync.WaitGroup
	errs := make(chan error, 8*20)
	for writer := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 20 {
				errs <- Write(path, []byte(fmt.Sprintf("writer %d\n", writer)), 0o600)
			}
		}()
		go func() {
			defer wg.Done()
			for range 20 {
				if err := Sweep(path); err != nil {
					t.Errorf("Sweep: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("write failed under a concurrent sweep: %v", err)
		}
	}
}

func TestSweepDirRemovesOnlyStaleStagingFilesOfTheSuffix(t *testing.T) {
	dir := t.TempDir()
	stale := []string{".ghost.jsonl.4242520"}
	aged := []string{".other.4242521", ".ghost.other.4242524", "ghost.jsonl"}
	kept := []string{".ghost.jsonl.keep", ".ghost.jsonl.4242522", ".jsonl.4242523"}
	for _, name := range slices.Concat(stale, aged, kept) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-time.Hour)
	for _, name := range slices.Concat(stale, aged) {
		if err := os.Chtimes(filepath.Join(dir, name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := SweepDir(dir, ".jsonl"); err != nil {
		t.Fatalf("SweepDir: %v", err)
	}
	for _, name := range stale {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("SweepDir left the stale staging file %s: %v", name, err)
		}
	}
	for _, name := range slices.Concat(aged, kept) {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("SweepDir removed %s, which is not a stale staging file of a .jsonl target: %v", name, err)
		}
	}
}

func TestSweepDirOfAMissingDirectoryIsNotAnError(t *testing.T) {
	if err := SweepDir(filepath.Join(t.TempDir(), "absent"), ".jsonl"); err != nil {
		t.Fatalf("SweepDir of a missing directory: %v", err)
	}
}
