//go:build unix

package tui

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

const draftLocksSupported = true

func acquireDraftLock(root *os.Root) (*os.File, error) {
	lock, err := root.OpenFile(draftLockName, os.O_CREATE|os.O_RDWR, draftLockMode)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(err, lock.Close())
	}
	return lock, nil
}

func runDirHeld(root *os.Root, name string) bool {
	lock, err := root.OpenFile(filepath.Join(name, draftLockName), os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	free := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
	if free {
		free = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) == nil
	}
	closeErr := lock.Close()
	return !free || closeErr != nil
}

func privateDir(info os.FileInfo) bool {
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}
