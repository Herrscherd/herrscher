//go:build !windows

package host

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestEnsureSocketFreeRefusesASocketAnotherDaemonServes(t *testing.T) {
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

	if err := ensureSocketFree(context.Background(), layout, path); !errors.Is(err, errSocketTaken) {
		t.Fatalf("ensureSocketFree on a live socket = %v, want errSocketTaken", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the live daemon's socket was removed: %v", err)
	}
}

func TestEnsureSocketFreeLeavesADeadSocketInPlace(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	path := filepath.Join(t.TempDir(), "dead.sock")
	deadSocket(t, path)
	if err := ensureSocketFree(context.Background(), layout, path); err != nil {
		t.Fatalf("ensureSocketFree on a dead socket = %v, want nil", err)
	}
	assertSocketFile(t, path)
}

func TestCreatingACommandSocketReplacesADeadFile(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	path := CommandSocketPathIn(layout, "t14")
	deadSocket(t, path)
	listener, err := openSocket(context.Background(), layout, path)
	if err != nil {
		t.Fatalf("openSocket over a dead file: %v", err)
	}
	t.Cleanup(func() { logCleanup(listener.Close()) })
	if !socketAnswers(context.Background(), layout, path) {
		t.Fatal("the socket created over a dead file does not answer")
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
	_, err := serveHubSockets(context.Background(), layout, "t3", &hub{warnings: io.Discard})
	if !errors.Is(err, errSocketTaken) {
		t.Fatalf("serveHubSockets with a live events socket = %v, want errSocketTaken", err)
	}
}

func TestServeHubSocketsStopsTheDaemonOnATakenCommandSocket(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	answeringListener(t, CommandSocketPathIn(layout, "t4"))
	_, err := serveHubSockets(context.Background(), layout, "t4", &hub{warnings: io.Discard})
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

func TestReleaseLeavesASocketANewerListenerBoundAtThePath(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	path := filepath.Join(t.TempDir(), "takeover.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	older, err := openSocket(ctx, layout, path)
	if err != nil {
		t.Fatal(err)
	}
	released := acceptCommands(ctx, older, &fakeDispatcher{out: "old"}, time.Second)
	newer, err := listenCommandSocket(context.Background(), layout, path)
	if err != nil {
		t.Fatalf("newer listen over the older socket: %v", err)
	}
	t.Cleanup(func() { logCleanup(newer.Close()) })
	cancel()
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("the older listener did not finish releasing after shutdown")
	}
	if !socketAnswers(context.Background(), layout, path) {
		t.Fatal("releasing the older listener disturbed the socket the newer one bound")
	}
}

func TestServeHubSocketsReleaseKeepsItsFilesInPlace(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	released, err := serveHubSockets(ctx, layout, "t6", &hub{warnings: io.Discard})
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
		assertSocketFile(t, path)
		if socketAnswers(context.Background(), layout, path) {
			t.Fatalf("socket %q still answers after its release was reported", path)
		}
	}
}

func TestRemoveSocketsRemovesOnlyTheSocketsOfThisInstance(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	h := &hub{runtime: layout, instanceID: "t12"}
	h.noteListened("alpha")
	h.noteListened("absent")
	owned := []string{
		CommandSocketPathIn(layout, "t12"),
		SessionCommandSocketPathIn(layout, "t12", "alpha"),
		control.SocketPath(layout, "t12", "alpha"),
	}
	for _, path := range owned {
		deadSocket(t, path)
	}
	outsider := CommandSocketPathIn(layout, "t13")
	deadSocket(t, outsider)
	regular := EventsSocketPathIn(layout, "t12")
	if err := os.WriteFile(regular, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := h.removeSockets(); err != nil {
		t.Fatalf("removeSockets: %v", err)
	}
	for _, path := range owned {
		assertAbsent(t, path)
	}
	assertSocketFile(t, outsider)
	if _, err := os.Lstat(regular); err != nil {
		t.Fatalf("a regular file at a socket path was removed: %v", err)
	}
}

func TestInstanceStopRemovesItsOwnSocketFiles(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	h := &hub{runtime: layout, instanceID: "t15", warnings: io.Discard}
	ctx, cancel := context.WithCancel(context.Background())
	released, err := serveHubSockets(ctx, layout, "t15", h)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("serveHubSockets did not report its sockets released after shutdown")
	}
	if err := h.removeSockets(); err != nil {
		t.Fatalf("removeSockets at instance stop: %v", err)
	}
	for _, path := range []string{CommandSocketPathIn(layout, "t15"), EventsSocketPathIn(layout, "t15")} {
		assertAbsent(t, path)
	}
}

func TestStopLiveReleasesEachSessionSocket(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	done := make(chan struct{})
	close(done)
	var cancelled, released int
	h := &hub{runtime: layout, instanceID: "t11", teardownGrace: time.Second, live: map[string]liveSession{
		"alpha": {
			cancel:   func() { cancelled++ },
			done:     done,
			released: done,
			release:  func() { released++ },
		},
	}}
	h.stopLive()
	if cancelled != 1 || released != 1 {
		t.Fatalf("stopLive cancelled %d and released %d sessions, want 1 and 1", cancelled, released)
	}
}

func TestGoLiveAfterStopLiveOpensNothing(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	h := &hub{runtime: layout, instanceID: "t16", warnings: io.Discard, live: make(map[string]liveSession)}
	h.stopLive()
	err := h.goLive(context.Background(), state.Session{Name: "alpha"})
	if !errors.Is(err, errHubClosed) {
		t.Fatalf("goLive after stopLive = %v, want errHubClosed", err)
	}
	assertAbsent(t, control.SocketPath(layout, "t16", "alpha"))
	assertAbsent(t, SessionCommandSocketPathIn(layout, "t16", "alpha"))
	if err := h.removeSockets(); err != nil {
		t.Fatalf("removeSockets: %v", err)
	}
	assertAbsent(t, control.SocketPath(layout, "t16", "alpha"))
	assertAbsent(t, SessionCommandSocketPathIn(layout, "t16", "alpha"))
}

func TestGoLiveAfterRemoveSocketsOpensNothing(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	h := &hub{runtime: layout, instanceID: "t17", warnings: io.Discard, live: make(map[string]liveSession)}
	if err := h.removeSockets(); err != nil {
		t.Fatalf("removeSockets: %v", err)
	}
	err := h.goLive(context.Background(), state.Session{Name: "alpha"})
	if !errors.Is(err, errHubClosed) {
		t.Fatalf("goLive after removeSockets = %v, want errHubClosed", err)
	}
	assertAbsent(t, control.SocketPath(layout, "t17", "alpha"))
}

func TestDeadDaemonSocketsAreReadAsAnAbsentDaemon(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	deadSocket(t, CommandSocketPathIn(layout, "t10"))
	deadSocket(t, EventsSocketPathIn(layout, "t10"))
	ctx := context.Background()

	if _, err := DaemonDispatch(ctx, layout, "t10", []string{"status"}); !IsDaemonNotRunning(err) {
		t.Fatalf("DaemonDispatch on a dead command socket = %v, want daemon not running", err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	if _, handled, err := ForwardToDaemon(ctx, layout, statePath, "t10", []string{"status"}); handled || err != nil {
		t.Fatalf("ForwardToDaemon on a dead command socket = handled %v, err %v, want not handled and no error", handled, err)
	}
	if _, err := SubscribeDaemonEvents(ctx, layout, "t10"); !errors.Is(err, errDaemonEventsUnreachable) {
		t.Fatalf("SubscribeDaemonEvents on a dead events socket = %v, want errDaemonEventsUnreachable", err)
	}
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
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertSocketFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("socket file %q is gone: %v", path, err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("%q is not a socket file: mode %v", path, info.Mode())
	}
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%q is still on disk: %v", path, err)
	}
}
