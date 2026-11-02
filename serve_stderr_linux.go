//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func redirectStderr(path string) (func() error, error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	stderrFd := int(os.Stderr.Fd())
	saved, err := unix.Dup(stderrFd)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	if err := unix.Dup3(int(f.Fd()), stderrFd, 0); err != nil {
		return nil, errors.Join(err, unix.Close(saved), f.Close())
	}
	return func() error {
		dupErr := unix.Dup3(saved, stderrFd, 0)
		return errors.Join(dupErr, unix.Close(saved), f.Close())
	}, nil
}
