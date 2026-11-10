//go:build unix

package host

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func instanceLockPath(instanceID string) (string, error) {
	dir, err := runtimedir.Private()
	if err != nil {
		return "", runtimeDirError(err)
	}
	return filepath.Join(dir, "lock-"+runtimedir.Key(CommandSocketPath(instanceID))+".lock"), nil
}

func openLockFile(path string) (*os.File, error) {
	file, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, ownerOnlyFileMode)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		closeRead(file)
		return nil, err
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Getuid() {
		closeRead(file)
		return nil, errLockNotOwned
	}
	return file, nil
}

func tryLock(f *os.File) (bool, error) {
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
