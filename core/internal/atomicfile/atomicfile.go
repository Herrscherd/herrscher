// Package atomicfile replaces a file in one step, so a reader sees the old
// content or the new one and never a partial write. The staged file is removed
// when any step fails.
package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Replacer moves a staged file onto its final path.
type Replacer func(oldPath, newPath string) error

// Write replaces path with data and gives the new file perm.
func Write(path string, data []byte, perm os.FileMode) error {
	return WriteWith(path, data, perm, os.Rename)
}

// WriteWith is Write with its rename step supplied by the caller, so a test can
// make the rename fail.
func WriteWith(path string, data []byte, perm os.FileMode, rename Replacer) error {
	staging, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := staging.Name()
	if err := stage(staging, data, perm); err != nil {
		return errors.Join(err, remove(name))
	}
	if err := rename(name, path); err != nil {
		return errors.Join(err, remove(name))
	}
	return nil
}

func stage(staging *os.File, data []byte, perm os.FileMode) error {
	_, writeErr := staging.Write(data)
	return errors.Join(writeErr, staging.Chmod(perm), staging.Close())
}

func remove(name string) error {
	if err := os.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
