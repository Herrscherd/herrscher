package host

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func blockStateSaves(t *testing.T, statePath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows ignores directory modes, so a failed state save cannot be produced")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root, where directory modes do not block writes, so a failed state save cannot be produced")
	}
	dir := filepath.Dir(statePath)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Error(err)
		}
	})
}

func unblockStateSaves(t *testing.T, statePath string) {
	t.Helper()
	if err := os.Chmod(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
}
