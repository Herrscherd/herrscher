package runtimedir

import (
	"errors"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
)

func makeShortRoot() (string, error) {
	return os.MkdirTemp("/tmp", "rt")
}

func shortRoot(t *testing.T) string {
	t.Helper()
	root, err := makeShortRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	return root
}

func shortLayout(t *testing.T) Layout {
	t.Helper()
	layout, err := At(path.Join(shortRoot(t), "h"))
	if err != nil {
		t.Fatal(err)
	}
	return layout
}

func TestResolveUsesTheSystemdRuntimeDirWhenItIsPrivate(t *testing.T) {
	runUser := shortRoot(t)
	uidDir := path.Join(runUser, strconv.Itoa(os.Getuid()))
	if err := os.Mkdir(uidDir, 0o700); err != nil {
		t.Fatal(err)
	}
	layout, err := resolveUnder(os.Getuid(), searchRoots{runUser: runUser, tmp: shortRoot(t)})
	if err != nil {
		t.Fatal(err)
	}
	if want := path.Join(uidDir, "herrscher"); layout.Dir() != want {
		t.Fatalf("Resolve().Dir() = %q, want %q", layout.Dir(), want)
	}
}

func TestResolveFallsBackToTmpWhenTheRuntimeDirIsOpen(t *testing.T) {
	runUser := shortRoot(t)
	uidDir := path.Join(runUser, strconv.Itoa(os.Getuid()))
	if err := os.Mkdir(uidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(uidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tmp := shortRoot(t)
	layout, err := resolveUnder(os.Getuid(), searchRoots{runUser: runUser, tmp: tmp})
	if err != nil {
		t.Fatal(err)
	}
	if want := DirOf(tmp, os.Getuid()); layout.Dir() != want {
		t.Fatalf("Resolve().Dir() = %q, want the temp fallback %q", layout.Dir(), want)
	}
}

func TestResolveFallsBackToTmpWhenThereIsNoRuntimeDir(t *testing.T) {
	tmp := shortRoot(t)
	layout, err := resolveUnder(os.Getuid(), searchRoots{runUser: path.Join(tmp, "missing"), tmp: tmp})
	if err != nil {
		t.Fatal(err)
	}
	if want := DirOf(tmp, os.Getuid()); layout.Dir() != want {
		t.Fatalf("Resolve().Dir() = %q, want %q", layout.Dir(), want)
	}
}

func TestResolveRejectsEmptyAndRelativeRoots(t *testing.T) {
	good := shortRoot(t)
	for _, roots := range []searchRoots{
		{runUser: "", tmp: good},
		{runUser: good, tmp: ""},
		{runUser: "run/user", tmp: good},
		{runUser: good, tmp: "tmp"},
	} {
		_, err := resolveUnder(os.Getuid(), roots)
		var rootErr *RootError
		if !errors.As(err, &rootErr) {
			t.Fatalf("Resolve(%+v) = %v, want a RootError", roots, err)
		}
	}
}

func TestAtRejectsEmptyAndRelativeDirectories(t *testing.T) {
	for _, dir := range []string{"", "relative/dir"} {
		_, err := At(dir)
		var rootErr *RootError
		if !errors.As(err, &rootErr) {
			t.Fatalf("At(%q) = %v, want a RootError", dir, err)
		}
	}
}

func TestAtRejectsADirectoryTooLongForASocketName(t *testing.T) {
	_, err := At("/" + strings.Repeat("d", MaxUnixSocketPath))
	var rootErr *RootError
	if !errors.As(err, &rootErr) {
		t.Fatalf("At over the limit = %v, want a RootError", err)
	}
}

func TestProductionIsTheResolvedLayoutOfThisUser(t *testing.T) {
	want, err := resolveUnder(os.Getuid(), productionRoots())
	if err != nil {
		t.Fatal(err)
	}
	if got := Production().Dir(); got != want.Dir() {
		t.Fatalf("Production().Dir() = %q, want %q", got, want.Dir())
	}
}

func TestSocketFitsTheUnixLimitWhateverTheSessionLength(t *testing.T) {
	layout := shortLayout(t)
	got := layout.Socket("herrscher-command-" + strings.Repeat("s", 120))
	if len(got) > MaxUnixSocketPath {
		t.Fatalf("socket is %d bytes, over the %d sun_path holds: %q", len(got), MaxUnixSocketPath, got)
	}
	if path.Dir(got) != layout.Dir() || !strings.HasPrefix(path.Base(got), hashedPrefix) {
		t.Fatalf("a socket that does not fit must be hashed inside the private dir, got %q", got)
	}
}

func TestShortSocketKeepsItsReadableName(t *testing.T) {
	layout := shortLayout(t)
	want := path.Join(layout.Dir(), "herrscher-events.sock")
	if got := layout.Socket("herrscher-events"); got != want {
		t.Fatalf("Socket = %q, want %q", got, want)
	}
}

func TestStemOutsideTheHerrscherPrefixAlwaysTakesTheHashedName(t *testing.T) {
	layout := shortLayout(t)
	if got := path.Base(layout.Socket("events")); !strings.HasPrefix(got, hashedPrefix) {
		t.Fatalf("a stem without the herrscher- prefix got the readable name %q", got)
	}
}

func TestHashedNameCannotCollideWithAPlainOne(t *testing.T) {
	layout := shortLayout(t)
	hashed := layout.Socket("herrscher-command-" + strings.Repeat("s", 120))
	crafted := "hs-" + strings.TrimSuffix(path.Base(hashed), socketSuffix)[len(hashedPrefix):]
	if got := layout.Socket(crafted); got == hashed {
		t.Fatalf("a session named %q collides with another session's hashed socket %q", crafted, hashed)
	}
}

func TestInstanceLockIsKeyedByInstanceIDInsideThePrivateDir(t *testing.T) {
	layout := shortLayout(t)
	first, second := layout.Lock("alpha"), layout.Lock("beta")
	if first == second {
		t.Fatalf("two instances share the lock %q", first)
	}
	if path.Dir(first) != layout.Dir() {
		t.Fatalf("lock %q is outside the private dir %q", first, layout.Dir())
	}
	if layout.Lock("alpha") != first {
		t.Fatal("the lock path of an instance is not stable")
	}
}

func TestPrivateCreatesAnOwnerOnlyDirectory(t *testing.T) {
	layout := shortLayout(t)
	dir, err := layout.Private()
	if err != nil {
		t.Fatalf("Private: %v", err)
	}
	if dir != layout.Dir() {
		t.Fatalf("Private = %q, want %q", dir, layout.Dir())
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
	layout := shortLayout(t)
	target := t.TempDir()
	if err := os.Symlink(target, layout.Dir()); err != nil {
		t.Fatal(err)
	}
	_, err := layout.Private()
	var unsafe *UnsafeError
	if !errors.As(err, &unsafe) || unsafe.Kind != KindSymlink {
		t.Fatalf("Private over a symlink = %v, want an UnsafeError of kind symlink", err)
	}
	if err := layout.Check(path.Join(layout.Dir(), "x.sock")); err == nil {
		t.Fatal("Check accepted a socket inside a symlinked private dir")
	}
}

func TestPrivateRefusesAGroupReadableDirectoryWithoutChangingIt(t *testing.T) {
	layout := shortLayout(t)
	if err := os.Mkdir(layout.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(layout.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := layout.Private()
	var unsafe *UnsafeError
	if !errors.As(err, &unsafe) || unsafe.Kind != KindOpenAccess {
		t.Fatalf("Private over a 0755 dir = %v, want an UnsafeError of kind open_access", err)
	}
	info, statErr := os.Stat(layout.Dir())
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("Private changed the mode of a foreign-looking dir to %o", info.Mode().Perm())
	}
}

func TestCheckIgnoresPathsOutsideThePrivateDir(t *testing.T) {
	layout := shortLayout(t)
	if err := os.Mkdir(layout.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := layout.Check(path.Join(path.Dir(layout.Dir()), "elsewhere.sock")); err != nil {
		t.Fatalf("Check of a path outside the private dir = %v, want nil", err)
	}
}
