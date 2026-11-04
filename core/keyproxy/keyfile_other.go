//go:build !unix

package keyproxy

import (
	"io/fs"
	"os"
	"path/filepath"
)

func openKeyFile(path string) (*os.File, error) {
	return os.Open(filepath.Clean(path))
}

func checkKeyDirectory(string, fs.FileInfo) error {
	return nil
}
