//go:build !unix

package keyproxy

import (
	"os"
	"path/filepath"
)

func openKeyFile(path string) (*os.File, error) {
	return os.Open(filepath.Clean(path))
}
