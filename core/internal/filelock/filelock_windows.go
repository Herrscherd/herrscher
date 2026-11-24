//go:build windows

package filelock

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const lockedOffset = 1 << 20

// Open opens or creates the lock file at path.
func Open(path string) (*os.File, error) {
	return os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_RDWR, lockFileMode)
}

// TryLock takes the exclusive lock of f without waiting. It reports false when
// another process holds it.
func TryLock(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{Offset: lockedOffset})
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		return false, nil
	default:
		return false, err
	}
}
