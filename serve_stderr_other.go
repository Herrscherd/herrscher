//go:build !linux

package main

func redirectStderr(path string) (func() error, error) {
	return func() error { return nil }, nil
}
