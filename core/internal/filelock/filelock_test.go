package filelock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestAcquireHoldsOffASecondAcquireUntilRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".lock")
	release, err := Acquire(t.Context(), path)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err = Acquire(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Acquire while held = %v, want the context error", err)
	}

	release()
	again, err := Acquire(t.Context(), path)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	again()
}

func TestAcquireCreatesTheLockFileOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes carry no owner-only meaning on windows")
	}
	path := filepath.Join(t.TempDir(), ".lock")
	release, err := Acquire(t.Context(), path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != lockFileMode {
		t.Fatalf("lock file mode = %o, want %o", mode, lockFileMode)
	}
}
