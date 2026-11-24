//go:build unix || windows

package filelock

import (
	"path/filepath"
	"testing"
)

func TestTryLockReportsALockHeldByAnotherOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".lock")
	first, err := Open(path)
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}
	if locked, lockErr := TryLock(first); lockErr != nil || !locked {
		t.Fatalf("TryLock of a free lock = %v, %v; want true, nil", locked, lockErr)
	}

	second, err := Open(path)
	if err != nil {
		t.Fatalf("Open second: %v", err)
	}
	if locked, err := TryLock(second); err != nil || locked {
		t.Fatalf("TryLock while another open holds it = %v, %v; want false, nil", locked, err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}
	if locked, err := TryLock(second); err != nil || !locked {
		t.Fatalf("TryLock after release = %v, %v; want true, nil", locked, err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("Close second: %v", err)
	}
}
