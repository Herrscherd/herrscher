//go:build unix

package tui

import (
	"errors"
	"os"
	"syscall"
)

const (
	draftLocksSupported = true
	draftLockMode       = 0o600
)

func openDraftLock(dir string, flag int, perm os.FileMode) (*os.File, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	lock, openErr := root.OpenFile(draftLockName, flag, perm)
	return lock, errors.Join(openErr, root.Close())
}

func lockRunDir(dir string) (*os.File, error) {
	lock, err := openDraftLock(dir, os.O_CREATE|os.O_RDWR, draftLockMode)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(err, lock.Close())
	}
	return lock, nil
}

func runDirHeld(dir string) bool {
	lock, err := openDraftLock(dir, os.O_RDWR, 0)
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
