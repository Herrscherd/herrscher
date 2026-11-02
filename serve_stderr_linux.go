//go:build linux

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func redirectStderr(path string) (restore func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	stderrFd := int(os.Stderr.Fd())
	saved, err := unix.Dup(stderrFd)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := unix.Dup3(int(f.Fd()), stderrFd, 0); err != nil {
		_ = unix.Close(saved)
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = unix.Dup3(saved, stderrFd, 0)
		_ = unix.Close(saved)
		_ = f.Close()
	}, nil
}
