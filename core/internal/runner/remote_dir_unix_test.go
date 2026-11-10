//go:build unix

package runner

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

func runRemoteDirScript(t *testing.T, root string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := exec.CommandContext(context.Background(), "sh", "-c", RemoteRuntimeDirScript(root))
	cmd.Stdout = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

func TestRemoteRuntimeDirScriptCreatesAPrivateDir(t *testing.T) {
	root := t.TempDir()
	got, err := runRemoteDirScript(t, root)
	if err != nil {
		t.Fatalf("script: %v", err)
	}
	want := filepath.Join(root, "herrscher-"+strconv.Itoa(os.Getuid()))
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

func TestRemoteRuntimeDirScriptRefusesAnOpenDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "herrscher-"+strconv.Itoa(os.Getuid()))
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runRemoteDirScript(t, root); err == nil {
		t.Fatal("script accepted a directory open to its group and others")
	}
}

func TestRemoteRuntimeDirScriptRefusesASymlink(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "herrscher-"+strconv.Itoa(os.Getuid()))); err != nil {
		t.Fatal(err)
	}
	if _, err := runRemoteDirScript(t, root); err == nil {
		t.Fatal("script followed a symlink planted at the private dir")
	}
}
