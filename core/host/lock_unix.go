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

func lockInstance(layout runtimedir.Layout, instanceID string) (func(), error) {
	path := layout.Lock(instanceID)
	if err := ensureRuntimeDir(layout, path); err != nil {
		return nil, err
	}
	release, err := takeLock(path)
	if _, ok := errors.AsType[*lockTakenError](err); ok {
		return nil, wrapHost(errInstanceLocked, "daemon.instance_locked", map[string]any{pathParam: path})
	}
	return release, err
}

func openLockFile(path string) (*os.File, error) {
	file, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, ownerOnlyFileMode)
	if errors.Is(err, syscall.ELOOP) {
		return nil, errLockSymlink
	}
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
