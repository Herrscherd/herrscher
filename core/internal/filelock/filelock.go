// Package filelock takes exclusive advisory locks on files that several
// processes share. The platform primitive lives here once, so every lock of
// the host and of its credentials is taken the same way on every system.
package filelock

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"
)

const (
	lockFileMode = os.FileMode(0o600)
	pollInterval = 20 * time.Millisecond
)

// ErrNotOwned reports a lock file that another user owns.
var ErrNotOwned = errors.New("the lock file is not owned by this user")

// ErrSymlink reports a lock path that is a symbolic link.
var ErrSymlink = errors.New("the lock file is a symbolic link")

// Acquire waits until the exclusive lock of path is free and returns the
// function that releases it. It stops with the error of ctx when ctx ends
// first.
func Acquire(ctx context.Context, path string) (func(), error) {
	return acquire(ctx, path, TryLock)
}

// AcquireShared waits until no exclusive lock holds path and takes a shared
// one, which other shared holders may hold at the same time. It returns the
// function that releases it, and stops with the error of ctx when ctx ends
// first.
func AcquireShared(ctx context.Context, path string) (func(), error) {
	return acquire(ctx, path, TryLockShared)
}

func acquire(ctx context.Context, path string, try func(*os.File) (bool, error)) (func(), error) {
	file, err := Open(path)
	if err != nil {
		return nil, err
	}
	for {
		locked, lockErr := try(file)
		if lockErr != nil {
			closeFile(file)
			return nil, lockErr
		}
		if locked {
			return func() { closeFile(file) }, nil
		}
		select {
		case <-ctx.Done():
			closeFile(file)
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func closeFile(file *os.File) {
	if err := file.Close(); err != nil {
		slog.Warn("close lock file", "err", err)
	}
}
