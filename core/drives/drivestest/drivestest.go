// Package drivestest installs a fake rclone for the tests of the drives and of the verbs that use them.
package drivestest

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

//go:embed rclone.sh
var files embed.FS

const (
	fileMode = 0o600
	dirMode  = 0o700
)

// Rclone is an installed fake rclone: Command runs it, Root holds one directory
// per remote, and Log is the file where each run appends its arguments and the
// RCLONE_ variables it saw.
type Rclone struct {
	Command string
	Root    string
	Log     string
}

// Install writes the fake rclone in a temporary directory and points the environment of the test at it.
func Install(tb testing.TB) Rclone {
	tb.Helper()
	script, err := files.ReadFile("rclone.sh")
	if err != nil {
		tb.Fatalf("read fake rclone: %v", err)
	}
	dir := tb.TempDir()
	path := filepath.Join(dir, "rclone.sh")
	if err := os.WriteFile(path, script, fileMode); err != nil {
		tb.Fatalf("write fake rclone: %v", err)
	}
	fake := Rclone{Command: "sh " + path, Root: filepath.Join(dir, "remotes"), Log: filepath.Join(dir, "rclone.log")}
	if err := os.MkdirAll(fake.Root, dirMode); err != nil {
		tb.Fatalf("make fake remotes: %v", err)
	}
	tb.Setenv("FAKE_RCLONE_ROOT", fake.Root)
	tb.Setenv("FAKE_RCLONE_LOG", fake.Log)
	return fake
}

// Put stores content at path of remote.
func (r Rclone) Put(tb testing.TB, remote, path, content string) {
	tb.Helper()
	full := filepath.Join(r.Root, remote, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), dirMode); err != nil {
		tb.Fatalf("make fake folder: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), fileMode); err != nil {
		tb.Fatalf("write fake file: %v", err)
	}
}

// Get reads path of remote, empty when it is absent.
func (r Rclone) Get(tb testing.TB, remote, path string) string {
	tb.Helper()
	raw, err := os.ReadFile(filepath.Clean(filepath.Join(r.Root, remote, filepath.FromSlash(path))))
	if err != nil {
		return ""
	}
	return string(raw)
}

// Runs is every argument line the fake recorded.
func (r Rclone) Runs(tb testing.TB) []string {
	tb.Helper()
	raw, err := os.ReadFile(r.Log)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// Hold makes every cat of the fake wait until Release, so a test can see what
// another run does meanwhile.
type Hold struct {
	path string
}

// HoldReads installs a hold on the cat runs of the fake.
func (r Rclone) HoldReads(tb testing.TB) Hold {
	tb.Helper()
	hold := Hold{path: filepath.Join(filepath.Dir(r.Log), "hold")}
	tb.Setenv("FAKE_RCLONE_HOLD", hold.path)
	return hold
}

// Started reports whether a cat reached the hold.
func (h Hold) Started() bool {
	_, err := os.Stat(h.path + ".started")
	return err == nil
}

// Release lets every held cat go on.
func (h Hold) Release(tb testing.TB) {
	tb.Helper()
	if err := os.WriteFile(h.path, nil, fileMode); err != nil {
		tb.Fatalf("release the fake hold: %v", err)
	}
}
