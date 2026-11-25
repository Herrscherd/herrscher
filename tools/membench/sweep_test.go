//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const deadPID = 2147483646

func makeRun(t *testing.T, root, name string, pid int) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	if pid > 0 {
		if err := os.WriteFile(filepath.Join(dir, ownerFileName), []byte(strconv.Itoa(pid)), credentialsMode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestSweepRemovesOnlyOrphanedBenchRuns(t *testing.T) {
	root := t.TempDir()
	orphan := makeRun(t, root, "run-1001", deadPID)
	live := makeRun(t, root, "run-1002", os.Getpid())
	noOwner := makeRun(t, root, "run-1003", 0)
	foreign := makeRun(t, root, "keep-1004", deadPID)
	file := filepath.Join(root, "run-1005")
	if err := os.WriteFile(file, nil, credentialsMode); err != nil {
		t.Fatal(err)
	}
	if err := sweepOrphans(root); err != nil {
		t.Fatal(err)
	}
	for _, kept := range []string{live, noOwner, foreign, file} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s was removed: %v", kept, err)
		}
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("orphaned run %s still exists: %v", orphan, err)
	}
}

func TestSweepRemovesStaleUnownedRunsOnly(t *testing.T) {
	root := t.TempDir()
	stale := makeRun(t, root, "run-1101", 0)
	fresh := makeRun(t, root, "run-1102", 0)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := sweepOrphans(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale unowned run %s still exists: %v", stale, err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh unowned run %s was removed: %v", fresh, err)
	}
}

func TestWriteOwnerRecordsThisProcess(t *testing.T) {
	dir := makeRun(t, t.TempDir(), "run-2001", 0)
	if err := writeOwner(dir); err != nil {
		t.Fatal(err)
	}
	if orphaned(dir) {
		t.Error("a run owned by this live process was reported as orphaned")
	}
}

func TestBenchParentSweepsOrphansBeforeItsRun(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	benchRoot := filepath.Join(cache, benchDirName)
	orphan := makeRun(t, benchRoot, "run-3001", deadPID)
	parent, err := benchParent()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(parent); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("orphaned run %s survived the startup sweep: %v", orphan, err)
	}
	if _, err := os.Stat(parent); err != nil {
		t.Errorf("the new run directory was not created: %v", err)
	}
}
