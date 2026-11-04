//go:build unix

package keyproxy

import (
	"os"
	"path/filepath"
	"syscall"
)

func openKeyFile(path string) (*os.File, error) {
	return os.OpenFile(filepath.Clean(path), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
