//go:build !linux

package main

func redirectStderr(path string) (restore func(), err error) {
	return func() {}, nil
}
