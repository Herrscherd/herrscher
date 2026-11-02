//go:build unix

package host

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLockStateRefusesASecondDaemon(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.json")
	release, err := LockState(state)
	if err != nil {
		t.Fatalf("first LockState: %v", err)
	}
	defer release()

	if _, err := LockState(state); err == nil {
		t.Fatal("second LockState succeeded; two daemons would serve the same state")
	} else if !strings.Contains(err.Error(), state) {
		t.Errorf("error = %q, want it to name the state file being contended", err)
	}
}

func TestLockStateNamesTheHoldingProcess(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.json")
	release, err := LockState(state)
	if err != nil {
		t.Fatalf("LockState: %v", err)
	}
	defer release()

	_, err = LockState(state)
	if err == nil {
		t.Fatal("second LockState succeeded")
	}
	if want := "pid " + strconv.Itoa(os.Getpid()); !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err, want)
	}
}

func TestReleasedLockCanBeRetaken(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.json")
	release, err := LockState(state)
	if err != nil {
		t.Fatalf("LockState: %v", err)
	}
	release()

	release2, err := LockState(state)
	if err != nil {
		t.Fatalf("LockState after release: %v", err)
	}
	release2()
}

func TestLockStateCreatesTheStateDirectory(t *testing.T) {
	state := filepath.Join(t.TempDir(), "fresh", "state.json")
	release, err := LockState(state)
	if err != nil {
		t.Fatalf("LockState in a missing directory: %v", err)
	}
	defer release()
	if _, err := os.Stat(StateLockPath(state)); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}
}

func TestStateLockSitsBesideTheStateFile(t *testing.T) {
	got := StateLockPath(filepath.Join("/var/lib/herrscher", "state.json"))
	want := filepath.Join("/var/lib/herrscher", ".state.json.lock")
	if got != want {
		t.Errorf("StateLockPath = %q, want %q", got, want)
	}
}
