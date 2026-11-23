package manage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

const (
	binaryName  = "herrscher"
	freshSuffix = ".new"
	staleSuffix = ".old"
	windowsOS   = "windows"
	windowsExt  = ".exe"
)

func runningBinary(dir string) string {
	if path, err := os.Executable(); err == nil {
		if resolved, evalErr := filepath.EvalSymlinks(path); evalErr == nil {
			return resolved
		}
		return path
	}
	name := binaryName
	if runtime.GOOS == windowsOS {
		name += windowsExt
	}
	return filepath.Join(dir, name)
}

func freshBinary(target string) string { return target + freshSuffix }

func swapBinary(target string) error {
	stale := target + staleSuffix
	if err := os.Rename(target, stale); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(freshBinary(target), target); err != nil {
		return errors.Join(err, putBack(stale, target))
	}
	if err := os.Remove(stale); err != nil && !errors.Is(err, fs.ErrNotExist) && runtime.GOOS != windowsOS {
		return err
	}
	return nil
}

func putBack(stale, target string) error {
	if err := os.Rename(stale, target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
