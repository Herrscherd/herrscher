package manage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type removeFunc func(string) error

const (
	binaryName     = "herrscher"
	freshSuffix    = ".new"
	staleSuffix    = ".old"
	windowsOS      = "windows"
	windowsExt     = ".exe"
	goRunDirPrefix = "go-build"
	parkedInfix    = "-parked-"
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
		if isGoRunDir(part) {
			return true
		}
	}
	return false
}

func isGoRunDir(name string) bool {
	digits, ok := strings.CutPrefix(name, goRunDirPrefix)
	if !ok || digits == "" {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func freshBinary(target string) string { return target + freshSuffix }

func staleBinary(target string) string { return target + staleSuffix }

func swapBinary(target string) error { return swapBinaryWith(target, os.Remove) }

func swapBinaryWith(target string, remove removeFunc) error {
	stale := staleBinary(target)
	if err := sweepParked(stale, remove); err != nil {
		return err
	}
	if err := clearStale(stale, remove); err != nil {
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

func clearStale(stale string, remove removeFunc) error {
	err := remove(stale)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	return park(stale)
}

func park(stale string) error {
	slot, err := os.CreateTemp(filepath.Dir(stale), filepath.Base(stale)+parkedInfix+"*")
	if err != nil {
		return err
	}
	if err := slot.Close(); err != nil {
		return err
	}
	return os.Rename(stale, slot.Name())
}

func sweepParked(stale string, remove removeFunc) error {
	entries, err := os.ReadDir(filepath.Dir(stale))
	if err != nil {
		return err
	}
	prefix := filepath.Base(stale) + parkedInfix
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		err := remove(filepath.Join(filepath.Dir(stale), entry.Name()))
		if err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrPermission) {
			return err
		}
	}
	return nil
}

func putBack(stale, target string) error {
	if err := os.Rename(stale, target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
