//go:build !windows

package control_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

func TestAcceptReplacesADeadControlSocketFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.sock")
	deadControlSocket(t, path)
	acceptor, err := control.Accept(context.Background(), path)
	if err != nil {
		t.Fatalf("Accept over a dead socket file: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := acceptor.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	conn, err := control.Dial(path)
	if err != nil {
		t.Fatalf("Dial the replacement control socket: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseLeavesTheControlSocketFileInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.sock")
	acceptor, err := control.Accept(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if closeErr := acceptor.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("the control socket file is gone after Close: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("%q is not a socket file after Close", path)
	}
	if conn, dialErr := control.Dial(path); dialErr == nil {
		logClose(t, conn)
		t.Fatal("a dead control socket file still answers after Close")
	}
}

func deadControlSocket(t *testing.T, path string) {
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
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
}

func logClose(t *testing.T, conn *control.Conn) {
	t.Helper()
	if err := conn.Close(); err != nil {
		t.Error(err)
	}
}
