//go:build !unix && !windows

package filelock

import "os"

// TryLock reports the lock as taken: this platform offers no file lock here.
func TryLock(*os.File) (bool, error) { return true, nil }
