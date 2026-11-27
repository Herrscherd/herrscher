package forgecreds

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func requireEnforcedModes(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce file and directory modes, so a permission failure cannot be produced")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores file and directory modes, so a permission failure cannot be produced")
	}
}

func blockWrites(t *testing.T, store *Store) {
	t.Helper()
	requireEnforcedModes(t)
	if err := store.ensureDir(); err != nil {
		t.Fatal(err)
	}
	for _, lock := range []string{store.lockPath(), store.renewLockPath(mustHost(t, "github.com"))} {
		file, err := os.OpenFile(filepath.Clean(lock), os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatalf("create lock %s: %v", lock, err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	setDirMode(t, store.dir, 0o500)
}

func allowWrites(t *testing.T, store *Store) {
	t.Helper()
	setDirMode(t, store.dir, 0o700)
}

func setFileMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func setDirMode(t *testing.T, dir string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Errorf("restore mode of %s: %v", dir, err)
		}
	})
}
