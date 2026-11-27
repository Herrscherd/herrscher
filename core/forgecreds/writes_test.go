package forgecreds

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func blockWrites(t *testing.T, store *Store) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce directory modes, so a read-only directory cannot refuse a write")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes through read-only directories, so the write cannot be refused")
	}
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
