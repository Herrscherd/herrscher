//go:build unix

package host

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLock takes a non-blocking exclusive flock. It reports false when the lock
// is held elsewhere, and an error only for a failure that is not contention —
// the caller must be able to tell "someone else is serving" from "the lock is
// unusable", since only the first has an explanation worth printing.
//
// flock is tied to the open file description, so the kernel drops it when the
// process exits: a killed or crashed daemon leaves nothing to clean up.
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
