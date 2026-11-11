//go:build !unix

package host

import "os"

func openLockFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, ownerOnlyFileMode)
}
