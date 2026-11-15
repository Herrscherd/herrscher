//go:build !linux

package main

func redirectStderrCapped(_ string, _ int64) (func() error, error) {
	return func() error { return nil }, nil
}
