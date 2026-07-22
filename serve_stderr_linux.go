//go:build linux

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// redirectStderr points the process's stderr file descriptor at path (created if
// absent, appended to) so operator logs — and any third-party writes straight to
// os.Stderr, like the discord gateway's fmt.Fprintf(os.Stderr, ...) — land in a
// file instead of the terminal. The foreground TUI owns the terminal; without
// this, those concurrent stderr writes paint over Bubble Tea's alt-screen and
// interleave with its colour/cursor probe handshake, leaking escape-sequence
// garbage into the flow. The redirect is done at the fd level (dup, not by
// reassigning the os.Stderr variable) so even code holding a captured *os.File
// writes to the log, and there is no data race on the global. restore re-points
// stderr at the original terminal.
func redirectStderr(path string) (restore func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	stderrFd := int(os.Stderr.Fd())
	saved, err := unix.Dup(stderrFd) // a fresh fd still pointing at the terminal
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	// Dup3(oldfd, newfd, 0): make newfd (stderr) alias oldfd (the log file).
	if err := unix.Dup3(int(f.Fd()), stderrFd, 0); err != nil {
		_ = unix.Close(saved)
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = unix.Dup3(saved, stderrFd, 0)
		_ = unix.Close(saved)
		_ = f.Close()
	}, nil
}
