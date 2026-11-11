//go:build unix

package runtimeguard

import (
	"errors"

	"golang.org/x/sys/unix"
)

func instanceLockHeld(path string) bool {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false
	}
	locked := unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB)
	if closeErr := unix.Close(descriptor); closeErr != nil {
		return false
	}
	return errors.Is(locked, unix.EWOULDBLOCK)
}
