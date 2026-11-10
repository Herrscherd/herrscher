//go:build unix

package host

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func TestInstanceLockLivesInThePrivateDirKeyedByInstance(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	release, err := lockInstance("t7")
	if err != nil {
		t.Fatalf("lockInstance: %v", err)
	}
	defer release()
	if _, err := os.Stat(layout.Lock("t7")); err != nil {
		t.Fatalf("the instance lock is not where the private dir keeps it: %v", err)
	}
	if filepath.Dir(layout.Lock("t7")) != layout.Dir() {
		t.Fatalf("instance lock %q is outside %q", layout.Lock("t7"), layout.Dir())
	}
}

func TestSecondDaemonOfOneInstanceIsRefused(t *testing.T) {
	isolatedRuntimeDir(t)
	first, err := lockInstance("t7")
	if err != nil {
		t.Fatalf("lockInstance: %v", err)
	}
	_, err = lockInstance("t7")
	if !errors.Is(err, errSocketTaken) {
		t.Fatalf("second lockInstance = %v, want errSocketTaken", err)
	}
	first()
	second, err := lockInstance("t7")
	if err != nil {
		t.Fatalf("lockInstance after release: %v", err)
	}
	second()
}

func TestInstanceLockRefusesASymlinkAtItsPath(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	if _, err := layout.Private(); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, layout.Lock("t8")); err != nil {
		t.Fatal(err)
	}
	_, err := lockInstance("t8")
	wrapped, ok := errors.AsType[hostWrapError](err)
	if !ok || wrapped.key != "lock.unsafe" {
		t.Fatalf("lockInstance over a symlink = %v, want the lock.unsafe error", err)
	}
	if !errors.Is(err, errLockSymlink) {
		t.Fatalf("lockInstance over a symlink = %v, want errLockSymlink underneath", err)
	}
	if content, readErr := os.ReadFile(victim); readErr != nil || string(content) != "keep" {
		t.Fatalf("the symlink target was touched: %q, %v", content, readErr)
	}
}

func TestRuntimeDirErrorNamesTheRemedyForItsKind(t *testing.T) {
	tests := map[runtimedir.Kind]string{
		runtimedir.KindSymlink:      "runtime.unsafe_dir.symlink",
		runtimedir.KindForeignOwner: "runtime.unsafe_dir.foreign_owner",
		runtimedir.KindOpenAccess:   "runtime.unsafe_dir.open_access",
		runtimedir.KindNotDir:       "runtime.unsafe_dir",
		runtimedir.KindOther:        "runtime.unsafe_dir",
	}
	for kind, want := range tests {
		if got := runtimeDirKey(kind); got != want {
			t.Fatalf("runtimeDirKey(%q) = %q, want %q", kind, got, want)
		}
	}
}
