// Package statesavestest makes the saves of a state file fail, for tests that need a state write to fail.
package statesavestest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Block makes every save of the state file at statePath fail by removing write access to its directory, and restores the access when the test ends.
func Block(tb testing.TB, statePath string) {
	tb.Helper()
	if runtime.GOOS == "windows" {
		tb.Skip("Windows ignores directory modes, so a failed state save cannot be produced")
	}
	if os.Geteuid() == 0 {
		tb.Skip("running as root, where directory modes do not block writes, so a failed state save cannot be produced")
	}
	dir := filepath.Dir(statePath)
	if err := os.Chmod(dir, 0o500); err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			tb.Error(err)
		}
	})
}

// Unblock restores write access to the directory of the state file at statePath, for a test that blocks the saves and then lets them through.
func Unblock(tb testing.TB, statePath string) {
	tb.Helper()
	if err := os.Chmod(filepath.Dir(statePath), 0o700); err != nil {
		tb.Fatal(err)
	}
}
