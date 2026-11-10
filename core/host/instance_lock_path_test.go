package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstanceLockLivesInTheUserCacheNotTheSharedTempDir(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(home, "cache")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("TMPDIR", filepath.Join(home, "tmp"))
	path := instanceLockPath("t7")
	if !strings.HasPrefix(path, cache+string(filepath.Separator)) {
		t.Fatalf("instance lock %q is not under the user cache %q", path, cache)
	}
	if strings.HasPrefix(path, os.TempDir()+string(filepath.Separator)) {
		t.Fatalf("instance lock %q sits in the shared temp dir, where another user can pre-create it", path)
	}
	release, err := lockInstance("t7")
	if err != nil {
		t.Fatalf("lockInstance: %v", err)
	}
	release()
}
