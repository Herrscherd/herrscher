//go:build !windows

package host

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func shortSocketRoot() (string, error) {
	return os.MkdirTemp("/tmp", "rsc")
}

func TestRemoteSocketCleanupRemovesOnlyTheNamedSocket(t *testing.T) {
	root, err := shortSocketRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if removeErr := os.RemoveAll(root); removeErr != nil {
			t.Error(removeErr)
		}
	})
	sock := filepath.Join(root, "herrscher-command-e2e-one.sock")
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logCleanup(listener.Close()) })
	plain := filepath.Join(root, "lock")
	if err := os.WriteFile(plain, []byte("held"), 0o600); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(root, "herrscher-command-dir")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatal(err)
	}

	run := func(path string) {
		t.Helper()
		argv := remoteSocketRemovalArgv(path)
		if out, err := exec.CommandContext(t.Context(), argv[0], argv[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("cleanup of %s: %v\n%s", path, err, out)
		}
	}
	run(sock)
	run(plain)
	run(subdir)

	if _, err := os.Lstat(sock); !os.IsNotExist(err) {
		t.Fatalf("the named socket survived the cleanup: %v", err)
	}
	if _, err := os.Stat(plain); err != nil {
		t.Fatalf("a regular file was removed by the socket cleanup: %v", err)
	}
	if _, err := os.Stat(subdir); err != nil {
		t.Fatalf("a directory was removed by the socket cleanup: %v", err)
	}
}
