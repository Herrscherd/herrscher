//go:build !linux

package main

// redirectStderr is a no-op on non-Linux platforms: the foreground TUI is a
// Linux/unix concern in practice, and the fd-level redirect uses a Linux-only
// syscall. Elsewhere the daemon keeps logging to the inherited stderr.
func redirectStderr(path string) (restore func(), err error) {
	return func() {}, nil
}
