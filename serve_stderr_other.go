//go:build !linux

package main

func redirectStderrCapped(path string, limit int64) (func() error, error) {
	return func() error { return nil }, nil
}
