//go:build !windows

package host

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestClaimSocketPathRefusesASocketAnotherDaemonServes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live.sock")
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logCleanup(listener.Close()) })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			logCleanup(conn.Close())
		}
	}()

	if err := claimSocketPath(context.Background(), path); !errors.Is(err, errSocketTaken) {
		t.Fatalf("claimSocketPath on a live socket = %v, want errSocketTaken", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the live daemon's socket was removed: %v", err)
	}
}

func TestClaimSocketPathClearsAStaleSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := claimSocketPath(context.Background(), path); err != nil {
		t.Fatalf("claimSocketPath on a stale file = %v, want nil", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale socket still present: %v", err)
	}
}

func TestADaemonRefusesAnInstanceAnotherDaemonServes(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := refuseTakenInstance(context.Background(), "t1"); err != nil {
		t.Fatalf("a free instance was refused: %v", err)
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", CommandSocketPath("t1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logCleanup(listener.Close()) })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			logCleanup(conn.Close())
		}
	}()
	if err := refuseTakenInstance(context.Background(), "t1"); !errors.Is(err, errSocketTaken) {
		t.Fatalf("refuseTakenInstance = %v, want errSocketTaken", err)
	}
}
