package manager

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func blockStateSaves(t *testing.T, statePath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce directory modes, so a save failure cannot be produced")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes, so a save failure cannot be produced")
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
