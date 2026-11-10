//go:build unix

package host

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const shortTempRoot = "/tmp"

func isolatedRuntimeDir(t *testing.T) {
	t.Helper()
	dir := filepath.Join(shortTempRoot, "rt"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("TMPDIR", t.TempDir())
}

func TestInstanceLockIsKeyedByTheCommandSocketNotTheCache(t *testing.T) {
	isolatedRuntimeDir(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	first, err := lockInstance("t7")
	if err != nil {
		t.Fatalf("lockInstance: %v", err)
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	_, err = lockInstance("t7")
	if !errors.Is(err, errSocketTaken) {
		t.Fatalf("second lockInstance with another cache dir = %v, want errSocketTaken", err)
	}
	first()
	second, err := lockInstance("t7")
	if err != nil {
		t.Fatalf("lockInstance after release: %v", err)
	}
	second()
}

func TestInstanceLockRefusesASymlinkAtItsPath(t *testing.T) {
	isolatedRuntimeDir(t)
	path, err := instanceLockPath("t8")
	if err != nil {
		t.Fatalf("instanceLockPath: %v", err)
	}
	victim := filepath.Join(t.TempDir(), "victim")
	if err = os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(victim, path); err != nil {
		t.Fatal(err)
	}
	_, err = lockInstance("t8")
	var wrapped hostWrapError
	if !errors.As(err, &wrapped) || wrapped.key != "lock.unsafe" {
		t.Fatalf("lockInstance over a symlink = %v, want the lock.unsafe error", err)
	}
	if content, readErr := os.ReadFile(victim); readErr != nil || string(content) != "keep" {
		t.Fatalf("the symlink target was touched: %q, %v", content, readErr)
	}
}
