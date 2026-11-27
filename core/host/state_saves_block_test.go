package host

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func blockStateSaves(t *testing.T, statePath string) func() {
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
	unblock := func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(unblock)
	return unblock
}
