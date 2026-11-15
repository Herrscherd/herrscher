//go:build !unix

package host

import (
	"os"
	"path/filepath"
)

func openLockFile(path string) (*os.File, error) {
	return os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_RDWR, ownerOnlyFileMode)
}
