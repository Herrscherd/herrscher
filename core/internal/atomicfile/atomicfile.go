// Package atomicfile replaces a file in one step, so readers never see a partial file.
package atomicfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Write replaces path with data and gives the new file perm.
func Write(path string, data []byte, perm os.FileMode) error {
	staging, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	name := staging.Name()
	if err := stage(staging, data, perm); err != nil {
		return fmt.Errorf("write %s: %w", path, errors.Join(err, remove(name)))
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("write %s: %w", path, errors.Join(err, remove(name)))
	}
	return nil
}

// Sweep removes the staging files a Write left beside path when its process died between staging and rename. Call it only before any writer of path starts.
func Sweep(path string) error {
	if path == "" {
		return nil
	}
	dir, base := filepath.Dir(path), filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("sweep %s: %w", path, err)
	}
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() && isStagingName(entry.Name(), base) {
			errs = append(errs, remove(filepath.Join(dir, entry.Name())))
		}
	}
	return errors.Join(errs...)
}

func isStagingName(name, base string) bool {
	suffix, found := strings.CutPrefix(name, "."+base+".")
	return found && isRandomSuffix(suffix)
}

func isRandomSuffix(suffix string) bool {
	if suffix == "" {
		return false
	}
	for _, digit := range suffix {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
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
