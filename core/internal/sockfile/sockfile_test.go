//go:build !windows

package sockfile_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
	"github.com/Herrscherd/herrscher/core/internal/sockfile"
)

func TestListenReplacesADeadSocketFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dead.sock")
	deadSocket(t, path)
	listener, err := sockfile.Listen(context.Background(), path, 0o600)
	if err != nil {
		t.Fatalf("Listen over a dead socket file: %v", err)
	}
	t.Cleanup(func() { closeListener(t, listener) })
	assertAnswers(t, path)
}

func TestListenBindsWithTheRequestedMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mode.sock")
	listener, err := sockfile.Listen(context.Background(), path, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeListener(t, listener) })
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket mode = %v, want 0600", perm)
	}
}

func TestListenAtTheSocketPathLimitReplacesADeadFile(t *testing.T) {
	dir := t.TempDir()
	name := strings.Repeat("n", runtimedir.MaxUnixSocketPath-len(dir)-1)
	path := filepath.Join(dir, name)
	if len(path) != runtimedir.MaxUnixSocketPath {
		t.Fatalf("test path is %d bytes, want %d", len(path), runtimedir.MaxUnixSocketPath)
	}
	deadSocket(t, path)
	listener, err := sockfile.Listen(context.Background(), path, 0o600)
	if err != nil {
		t.Fatalf("Listen at the socket path limit over a dead file: %v", err)
	}
	t.Cleanup(func() { closeListener(t, listener) })
	assertAnswers(t, path)
}

func TestListenLeavesNoStagedFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.sock")
	listener, err := sockfile.Listen(context.Background(), path, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeListener(t, listener) })
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || filepath.Join(dir, entries[0].Name()) != path {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("directory holds %v after Listen, want only the socket", names)
	}
}

func TestClosingTheListenerLeavesTheSocketFileInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kept.sock")
	listener, err := sockfile.Listen(context.Background(), path, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	closeListener(t, listener)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("the socket file is gone after Close: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("%q is not a socket file after Close", path)
	}
}

func deadSocket(t *testing.T, path string) {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	unix, ok := listener.(*net.UnixListener)
	if !ok {
		t.Fatalf("listener at %q is %T, want *net.UnixListener", path, listener)
	}
	unix.SetUnlinkOnClose(false)
	closeListener(t, listener)
}

func assertAnswers(t *testing.T, path string) {
	t.Helper()
	conn, err := (&net.Dialer{}).DialContext(context.Background(), "unix", path)
	if err != nil {
		t.Fatalf("dial %q: %v", path, err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
}

func closeListener(t *testing.T, listener net.Listener) {
	t.Helper()
	if err := listener.Close(); err != nil {
		t.Error(err)
	}
}
