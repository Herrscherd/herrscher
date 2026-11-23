//go:build !unix && !windows

package filelock

import (
	"os"
	"path/filepath"
)

// Open opens or creates the lock file at path.
func Open(path string) (*os.File, error) {
	return os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_RDWR, lockFileMode)
}
