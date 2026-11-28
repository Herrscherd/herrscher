//go:build !unix

package discovery

import (
	"os"
	"path/filepath"
)

func openSSHConfig(path string) (*os.File, error) {
	return os.Open(filepath.Clean(path))
}
