//go:build !windows

package devices

import (
	"errors"
	"os"
	"path/filepath"
)

func syncDirectory(dir string) error {
	handle, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return err
	}
	return errors.Join(handle.Sync(), handle.Close())
}
