package manage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	binaryName     = "herrscher"
	freshSuffix    = ".new"
	staleSuffix    = ".old"
	windowsOS      = "windows"
	windowsExt     = ".exe"
	goRunDirPrefix = "go-build"
)

func runningBinary(dir string) string {
	if path, err := os.Executable(); err == nil && !builtByGoRun(path) {
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

func builtByGoRun(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.HasPrefix(part, goRunDirPrefix) {
			return true
		}
	}
	return false
}

func freshBinary(target string) string { return target + freshSuffix }

func staleBinary(target string) string { return target + staleSuffix }

func swapBinary(target string) error {
	stale := staleBinary(target)
	if err := os.Remove(stale); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(target, stale); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(freshBinary(target), target); err != nil {
		return errors.Join(err, putBack(stale, target))
	}
	return nil
}

func putBack(stale, target string) error {
	if err := os.Rename(stale, target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
