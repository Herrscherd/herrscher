// Package atomicfile replaces a file in one step, so readers never see a partial file.
package atomicfile

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const abandonedAfter = 10 * time.Minute

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

// Sweep removes the staging files that a Write left beside path and that are at least ten minutes old, which means their writer died between staging and rename. A younger staging file may belong to a live writer, so Sweep keeps it and is safe to call while writers of path run. Sweep is not called on each save of a state file or each compaction of a transcript: the daemon sweeps those once at startup, since a directory read per save is not worth it on the hot path.
func Sweep(path string) error {
	if path == "" {
		return nil
	}
	base := filepath.Base(path)
	return sweepDir(filepath.Dir(path), func(name string) bool {
		return isStagingName(name, base)
	})
}

// SweepDir removes the stale staging files of every file in dir, with the same rules as Sweep. The daemon uses it at startup for the transcripts directory, so a transcript of a session no longer in state keeps no residue.
func SweepDir(dir string) error {
	return sweepDir(dir, isAnyStagingName)
}

func sweepDir(dir string, isStaging func(name string) bool) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("sweep %s: %w", dir, err)
	}
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() || !isStaging(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if time.Since(info.ModTime()) >= abandonedAfter {
			errs = append(errs, remove(filepath.Join(dir, entry.Name())))
		}
	}
	return errors.Join(errs...)
}

// SweepLogged runs Sweep and logs a failure at debug level, for callers that carry on whatever the sweep finds.
func SweepLogged(path string) {
	if err := Sweep(path); err != nil {
		slog.Debug("staging files not swept", "path", path, "err", err)
	}
}

func isStagingName(name, base string) bool {
	suffix, found := strings.CutPrefix(name, "."+base+".")
	return found && isRandomSuffix(suffix)
}

func isAnyStagingName(name string) bool {
	rest, found := strings.CutPrefix(name, ".")
	if !found {
		return false
	}
	dot := strings.LastIndexByte(rest, '.')
	return dot > 0 && isRandomSuffix(rest[dot+1:])
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
