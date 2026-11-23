//go:build unix

package filelock

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// Open opens or creates the lock file at path for the owner only. It refuses
// a symbolic link and a file owned by another user.
func Open(path string) (*os.File, error) {
	file, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, lockFileMode)
	if errors.Is(err, syscall.ELOOP) {
		return nil, ErrSymlink
	}
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		closeFile(file)
		return nil, err
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Getuid() {
		closeFile(file)
		return nil, ErrNotOwned
	}
	return file, nil
}

// TryLock takes the exclusive lock of f without waiting. It reports false when
// another process holds it. The lock is released when f is closed.
func TryLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, unix.EWOULDBLOCK):
		return false, nil
	default:
		return false, err
	}
}
