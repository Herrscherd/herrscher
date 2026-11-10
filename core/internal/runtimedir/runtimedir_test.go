package runtimedir

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const shortTempRoot = "/tmp"

func shortRuntimeDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(shortTempRoot, "rt"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	t.Setenv("XDG_RUNTIME_DIR", dir)
	return dir
}

func TestSocketFitsTheUnixLimitWhateverTheTempDirAndSessionLength(t *testing.T) {
	shortRuntimeDir(t)
	t.Setenv("TMPDIR", "/tmp/"+strings.Repeat("t", 60))
	stem := "herrscher-command-" + strings.Repeat("s", 120)
	got := Socket(stem)
	if len(got) > MaxUnixSocketPath {
		t.Fatalf("socket is %d bytes, over the %d sun_path holds: %q", len(got), MaxUnixSocketPath, got)
	}
	if !Holds(got) {
		t.Fatalf("a socket that does not fit the temp dir must live in the private dir, got %q", got)
	}
}

func TestShortSocketStaysInTheTempDir(t *testing.T) {
	shortRuntimeDir(t)
	t.Setenv("TMPDIR", filepath.Join(shortTempRoot, "short"))
	if got, want := Socket("herrscher-events"), filepath.Join(shortTempRoot, "short", "herrscher-events.sock"); got != want {
		t.Fatalf("Socket = %q, want %q", got, want)
	}
}

func TestHashedNameCannotCollideWithAPlainSessionName(t *testing.T) {
	shortRuntimeDir(t)
	t.Setenv("TMPDIR", "/tmp/"+strings.Repeat("t", 60))
	hashed := Socket("herrscher-command-" + strings.Repeat("s", 120))
	crafted := strings.TrimSuffix(filepath.Base(hashed), ".sock")
	plain := Socket(crafted)
	if plain == hashed {
		t.Fatalf("a session named %q collides with another session's hashed socket %q", crafted, hashed)
	}
	if filepath.Dir(plain) == filepath.Dir(hashed) {
		t.Fatalf("hashed sockets share a directory with plain ones: %q and %q", plain, hashed)
	}
}

func TestPrivateCreatesAnOwnerOnlyDirectory(t *testing.T) {
	base := shortRuntimeDir(t)
	dir, err := Private()
	if err != nil {
		t.Fatalf("Private: %v", err)
	}
	if dir != filepath.Join(base, "herrscher") {
		t.Fatalf("Private = %q, want the herrscher subdirectory of XDG_RUNTIME_DIR", dir)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("private dir mode %o, want 0700", info.Mode().Perm())
	}
}

func TestPrivateRefusesASymlinkInPlace(t *testing.T) {
	base := shortRuntimeDir(t)
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(base, "herrscher")); err != nil {
		t.Fatal(err)
	}
	_, err := Private()
	var unsafe *UnsafeError
	if !errors.As(err, &unsafe) || unsafe.Reason != "is a symbolic link" {
		t.Fatalf("Private over a symlink = %v, want an UnsafeError naming the symlink", err)
	}
	if err := Check(filepath.Join(base, "herrscher", "x.sock")); err == nil {
		t.Fatal("Check accepted a socket inside a symlinked private dir")
	}
}

func TestPrivateRefusesAGroupReadableDirectory(t *testing.T) {
	base := shortRuntimeDir(t)
	dir := filepath.Join(base, "herrscher")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Private()
	var unsafe *UnsafeError
	if !errors.As(err, &unsafe) || unsafe.Reason != "is accessible to its group or others" {
		t.Fatalf("Private over a 0755 dir = %v, want an UnsafeError on group access", err)
	}
}
