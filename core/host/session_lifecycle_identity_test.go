package host

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestGoDeadWaitsForRunSessionTeardown(t *testing.T) {
	cancelled := make(chan struct{})
	tornDown := make(chan struct{})
	returned := make(chan struct{})
	h := &hub{teardownGrace: defaultTeardownGrace, live: map[string]liveSession{
		"same-name": {
			cancel:  func() { close(cancelled) },
			done:    tornDown,
			release: func() {},
		},
	}}

	go func() {
		h.goDead("same-name")
		close(returned)
	}()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("goDead did not cancel the live session")
	}
	select {
	case <-returned:
		t.Fatal("goDead returned before RunSession teardown completed")
	default:
	}

	close(tornDown)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("goDead did not return after teardown completed")
	}
	h.mu.Lock()
	_, stillLive := h.live["same-name"]
	h.mu.Unlock()
	if stillLive {
		t.Fatal("torn-down session remains in the live map")
	}
}

func TestOldDriverCannotUnregisterSameNameReplacement(t *testing.T) {
	sessionTab := newSessionTable()
	name := "registry-" + newTurnID()
	oldDriver := newSessionDriver(name, nil, nil, nil)
	newDriver := newSessionDriver(name, nil, nil, nil)
	sessionTab.register(name, oldDriver)
	t.Cleanup(func() { sessionTab.unregister(name, newDriver) })

	sessionTab.register(name, newDriver)
	sessionTab.unregister(name, oldDriver)

	got := sessionTab.lookup(name)
	if got != newDriver {
		t.Fatalf("old teardown removed replacement driver: got %p want %p", got, newDriver)
	}
}

func TestSameNameSessionCanRebindAfterTeardown(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	approvalTab := newApprovalTable()
	sessionTab := newSessionTable()
	name := "rebind-" + newTurnID()
	h := newHub(
		state.NewState(filepath.Join(t.TempDir(), "state.json")),
		nil,
		nil,
		t.TempDir(),
		nil,
		nil,
		approvalTab,
		sessionTab,
	)
	h.runtime = layout
	t.Cleanup(func() { h.goDead(name) })

	if err := h.goLive(t.Context(), state.Session{Name: name, Incarnation: "old-incarnation"}); err != nil {
		t.Fatal(err)
	}
	awaitRegisteredIncarnation(t, sessionTab, name, "old-incarnation")
	h.goDead(name)

	if err := h.goLive(t.Context(), state.Session{Name: name, Incarnation: "new-incarnation"}); err != nil {
		t.Fatal(err)
	}
	awaitRegisteredIncarnation(t, sessionTab, name, "new-incarnation")
	conn, err := control.Dial(control.SocketPath(layout, "", name))
	if err != nil {
		t.Fatalf("replacement control listener is not reachable: %v", err)
	}
	logCleanup(conn.Close())
}

func awaitRegisteredIncarnation(t *testing.T, sessionTab *sessionTable, name, incarnation string) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(5 * time.Second)
	for {
		if registeredIncarnation(sessionTab, name) == incarnation {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("session %s never registered incarnation %s", name, incarnation)
		}
	}
}

func registeredIncarnation(sessionTab *sessionTable, name string) string {
	driver := sessionTab.lookup(name)
	if driver == nil {
		return ""
	}
	return driver.incarnation
}

func TestSessionSocketOutlivesTheContextThatCreatedIt(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	name := "outlive-" + newTurnID()
	h := newHub(
		state.NewState(filepath.Join(t.TempDir(), "state.json")),
		nil,
		nil,
		t.TempDir(),
		nil,
		nil,
		newApprovalTable(),
		newSessionTable(),
	)
	h.runtime = layout
	t.Cleanup(func() { h.goDead(name) })

	ctx, cancel := context.WithCancel(t.Context())
	if err := h.goLive(ctx, state.Session{Name: name, Incarnation: "incarnation"}); err != nil {
		t.Fatal(err)
	}
	cancel()
	<-time.After(200 * time.Millisecond)
	conn, err := control.Dial(control.SocketPath(layout, "", name))
	if err != nil {
		t.Fatalf("the session socket closed with the creating context: %v", err)
	}
	logCleanup(conn.Close())
}

func TestGoDeadWaitsForTheCommandSocketRelease(t *testing.T) {
	done := make(chan struct{})
	close(done)
	released := make(chan struct{})
	h := &hub{teardownGrace: 5 * time.Second, live: map[string]liveSession{
		"rebind": {cancel: func() {}, done: done, released: released, release: func() {}},
	}}
	returned := make(chan struct{})
	go func() {
		h.goDead("rebind")
		close(returned)
	}()
	select {
	case <-returned:
		t.Fatal("goDead returned before the command socket was released, a rebind would find it still answering")
	case <-time.After(100 * time.Millisecond):
	}
	close(released)
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("goDead did not return once the command socket was released")
	}
}

func TestRebindSurvivesAnOverrunTeardown(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	name := "overrun-" + newTurnID()
	sessionTab := newSessionTable()
	h := newHub(
		state.NewState(filepath.Join(t.TempDir(), "state.json")),
		nil,
		nil,
		t.TempDir(),
		nil,
		nil,
		newApprovalTable(),
		sessionTab,
	)
	h.runtime = layout
	h.teardownGrace = 0
	entered := make(chan struct{})
	holdTeardown := make(chan struct{})
	releaseTeardown := sync.OnceFunc(func() { close(holdTeardown) })
	var enterOnce sync.Once
	h.onSessionStopped = func() {
		enterOnce.Do(func() { close(entered) })
		<-holdTeardown
	}
	t.Cleanup(func() { h.goDead(name) })
	t.Cleanup(releaseTeardown)

	if err := h.goLive(t.Context(), state.Session{Name: name, Incarnation: "old-incarnation"}); err != nil {
		t.Fatal(err)
	}
	awaitRegisteredIncarnation(t, sessionTab, name, "old-incarnation")
	h.mu.Lock()
	oldDone := h.live[name].done
	h.mu.Unlock()

	overrun := make(chan struct{})
	go func() {
		h.goDead(name)
		close(overrun)
	}()
	<-entered
	select {
	case <-overrun:
	case <-time.After(5 * time.Second):
		t.Fatal("goDead did not return once the teardown overran its grace")
	}

	if err := h.goLive(t.Context(), state.Session{Name: name, Incarnation: "new-incarnation"}); err != nil {
		t.Fatalf("rebind while the old teardown is still held: %v", err)
	}
	releaseTeardown()
	<-oldDone

	conn, err := control.Dial(control.SocketPath(layout, "", name))
	if err != nil {
		t.Fatalf("the late teardown of the old session removed the replacement control socket: %v", err)
	}
	logCleanup(conn.Close())
}
