//go:build !windows

package host

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestADaemonRefusesAnInstanceWhoseEventsSocketIsTaken(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	answeringListener(t, EventsSocketPath("t2"))
	if err := refuseTakenInstance(context.Background(), "t2"); !errors.Is(err, errSocketTaken) {
		t.Fatalf("refuseTakenInstance with a live events socket = %v, want errSocketTaken", err)
	}
}

func TestServeHubSocketsStopsTheDaemonOnATakenEventsSocket(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	answeringListener(t, EventsSocketPath("t3"))
	err := serveHubSockets(context.Background(), "t3", &hub{})
	if !errors.Is(err, errSocketTaken) {
		t.Fatalf("serveHubSockets with a live events socket = %v, want errSocketTaken", err)
	}
}

func TestServeHubSocketsStopsTheDaemonOnATakenCommandSocket(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	answeringListener(t, CommandSocketPath("t4"))
	err := serveHubSockets(context.Background(), "t4", &hub{})
	if !errors.Is(err, errSocketTaken) {
		t.Fatalf("serveHubSockets with a live command socket = %v, want errSocketTaken", err)
	}
}

func TestLockInstanceRefusesASecondDaemonOfTheSameInstance(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	release, err := lockInstance("t5")
	if err != nil {
		t.Fatalf("first lockInstance: %v", err)
	}
	if _, err := lockInstance("t5"); !errors.Is(err, errSocketTaken) {
		t.Fatalf("second lockInstance = %v, want errSocketTaken", err)
	}
	release()
	again, err := lockInstance("t5")
	if err != nil {
		t.Fatalf("lockInstance after release: %v", err)
	}
	again()
}

func answeringListener(t *testing.T, path string) {
	t.Helper()
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
}

func TestShutdownLeavesASocketANewerDaemonOwns(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "takeover.sock")
	ctx, cancel := context.WithCancel(context.Background())
	listener, err := openCommandSocket(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	go acceptCommands(ctx, listener, &fakeDispatcher{out: "old"}, time.Second)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	newer, err := listenCommandSocket(context.Background(), path)
	if err != nil {
		t.Fatalf("newer daemon listen: %v", err)
	}
	t.Cleanup(func() { logCleanup(newer.Close()) })
	cancel()
	time.Sleep(50 * time.Millisecond)
	if !socketAnswers(context.Background(), path) {
		t.Fatal("the older daemon removed the socket the newer one owns on shutdown")
	}
}
