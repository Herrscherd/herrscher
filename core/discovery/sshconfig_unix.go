//go:build unix

package discovery

import (
	"os"
	"path/filepath"
	"syscall"
)

func openSSHConfig(path string) (*os.File, error) {
	return os.OpenFile(filepath.Clean(path), os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
