//go:build unix

package runtimedir

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func runRemoteDirScript(t *testing.T, runUser, tmp string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := exec.CommandContext(context.Background(), "sh", "-c", remoteDirScript(searchRoots{runUser: runUser, tmp: tmp}))
	cmd.Stdout = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

func TestRemoteDirScriptPrefersAPrivateRuntimeDir(t *testing.T) {
	runUser, tmp := t.TempDir(), t.TempDir()
	uidDir := filepath.Join(runUser, strconv.Itoa(os.Getuid()))
	if err := os.Mkdir(uidDir, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := runRemoteDirScript(t, runUser, tmp)
	if err != nil {
		t.Fatalf("script: %v", err)
	}
	if want := filepath.Join(uidDir, "herrscher"); got != want {
		t.Fatalf("script printed %q, want %q", got, want)
	}
}

func TestRemoteDirScriptFallsBackToTmpWithoutARuntimeDir(t *testing.T) {
	tmp := t.TempDir()
	got, err := runRemoteDirScript(t, filepath.Join(tmp, "missing"), tmp)
	if err != nil {
		t.Fatalf("script: %v", err)
	}
	want := filepath.Join(tmp, "herrscher-"+strconv.Itoa(os.Getuid()))
	if got != want {
		t.Fatalf("script printed %q, want %q", got, want)
	}
	info, err := os.Lstat(want)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode %o, want 0700", info.Mode().Perm())
	}
}

func TestRemoteDirScriptSkipsAnOpenRuntimeDir(t *testing.T) {
	runUser, tmp := t.TempDir(), t.TempDir()
	uidDir := filepath.Join(runUser, strconv.Itoa(os.Getuid()))
	if err := os.Mkdir(uidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(uidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := runRemoteDirScript(t, runUser, tmp)
	if err != nil {
		t.Fatalf("script: %v", err)
	}
	if want := filepath.Join(tmp, "herrscher-"+strconv.Itoa(os.Getuid())); got != want {
		t.Fatalf("script printed %q, want the temp fallback %q", got, want)
	}
}

func TestRemoteDirScriptRefusesAnOpenDir(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "herrscher-"+strconv.Itoa(os.Getuid()))
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runRemoteDirScript(t, filepath.Join(tmp, "missing"), tmp); err == nil {
		t.Fatal("script accepted a directory open to its group and others")
	}
}

func TestRemoteDirScriptRefusesASymlink(t *testing.T) {
	tmp := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(tmp, "herrscher-"+strconv.Itoa(os.Getuid()))); err != nil {
		t.Fatal(err)
	}
	if _, err := runRemoteDirScript(t, filepath.Join(tmp, "missing"), tmp); err == nil {
		t.Fatal("script followed a symlink planted at the private dir")
	}
}

func TestIsRemoteDirAcceptsOnlyTheTwoProductionShapes(t *testing.T) {
	for dir, want := range map[string]bool{
		"/run/user/1000/herrscher":  true,
		"/tmp/herrscher-1000":       true,
		"/run/user/abc/herrscher":   false,
		"/run/user/1000/other":      false,
		"/srv/herrscher-1000":       false,
		"/tmp/herrscher-1000/extra": false,
	} {
		if got := IsRemoteDir(dir); got != want {
			t.Fatalf("IsRemoteDir(%q) = %t, want %t", dir, got, want)
		}
	}
}
