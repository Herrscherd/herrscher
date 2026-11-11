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
	layout := isolatedRuntimeDir(t)
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

	if err := claimSocketPath(context.Background(), layout, path); !errors.Is(err, errSocketTaken) {
		t.Fatalf("claimSocketPath on a live socket = %v, want errSocketTaken", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the live daemon's socket was removed: %v", err)
	}
}

func TestClaimSocketPathClearsAStaleSocket(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	path := filepath.Join(t.TempDir(), "stale.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := claimSocketPath(context.Background(), layout, path); err != nil {
		t.Fatalf("claimSocketPath on a stale file = %v, want nil", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale socket still present: %v", err)
	}
}

func TestADaemonRefusesAnInstanceAnotherDaemonServes(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	if err := refuseTakenInstance(context.Background(), layout, "t1"); err != nil {
		t.Fatalf("a free instance was refused: %v", err)
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", CommandSocketPathIn(layout, "t1"))
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
	if err := refuseTakenInstance(context.Background(), layout, "t1"); !errors.Is(err, errSocketTaken) {
		t.Fatalf("refuseTakenInstance = %v, want errSocketTaken", err)
	}
}

func TestADaemonRefusesAnInstanceWhoseEventsSocketIsTaken(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	answeringListener(t, EventsSocketPathIn(layout, "t2"))
	if err := refuseTakenInstance(context.Background(), layout, "t2"); !errors.Is(err, errSocketTaken) {
		t.Fatalf("refuseTakenInstance with a live events socket = %v, want errSocketTaken", err)
	}
}

func TestServeHubSocketsStopsTheDaemonOnATakenEventsSocket(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	answeringListener(t, EventsSocketPathIn(layout, "t3"))
	_, err := serveHubSockets(context.Background(), layout, "t3", &hub{})
	if !errors.Is(err, errSocketTaken) {
		t.Fatalf("serveHubSockets with a live events socket = %v, want errSocketTaken", err)
	}
}

func TestServeHubSocketsStopsTheDaemonOnATakenCommandSocket(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	answeringListener(t, CommandSocketPathIn(layout, "t4"))
	_, err := serveHubSockets(context.Background(), layout, "t4", &hub{})
	if !errors.Is(err, errSocketTaken) {
		t.Fatalf("serveHubSockets with a live command socket = %v, want errSocketTaken", err)
	}
}

func TestLockInstanceRefusesASecondDaemonOfTheSameInstance(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	release, err := lockInstance(layout, "t5")
	if err != nil {
		t.Fatalf("first lockInstance: %v", err)
	}
	if _, secondErr := lockInstance(layout, "t5"); !errors.Is(secondErr, errInstanceLocked) {
		t.Fatalf("second lockInstance = %v, want errInstanceLocked", secondErr)
	}
	release()
	again, err := lockInstance(layout, "t5")
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
	layout := isolatedRuntimeDir(t)
	path := filepath.Join(t.TempDir(), "takeover.sock")
	ctx, cancel := context.WithCancel(context.Background())
	listener, err := openCommandSocket(ctx, layout, path)
	if err != nil {
		t.Fatal(err)
	}
	released := acceptCommands(ctx, listener, &fakeDispatcher{out: "old"}, time.Second)
	if removeErr := os.Remove(path); removeErr != nil {
		t.Fatal(removeErr)
	}
	newer, err := listenCommandSocket(context.Background(), layout, path)
	if err != nil {
		t.Fatalf("newer daemon listen: %v", err)
	}
	t.Cleanup(func() { logCleanup(newer.Close()) })
	cancel()
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("the older daemon did not finish releasing its socket after shutdown")
	}
	if !socketAnswers(context.Background(), layout, path) {
		t.Fatal("the older daemon removed the socket the newer one owns on shutdown")
	}
}

func TestServeHubSocketsReportReleasedOnlyOnceTheFilesAreGone(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	released, err := serveHubSockets(ctx, layout, "t6", &hub{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("serveHubSockets did not report its sockets released after shutdown")
	}
	for _, path := range []string{CommandSocketPathIn(layout, "t6"), EventsSocketPathIn(layout, "t6")} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("socket %q is still on disk after its release was reported: %v", path, statErr)
		}
	}
}
