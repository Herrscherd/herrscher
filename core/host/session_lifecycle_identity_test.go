package host

import (
	"context"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestGoDeadWaitsForRunSessionTeardown(t *testing.T) {
	cancelled := make(chan struct{})
	tornDown := make(chan struct{})
	returned := make(chan struct{})
	h := &hub{warnings: io.Discard, teardownGrace: defaultTeardownGrace, live: map[string]liveSession{
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
	oldDriver := newSessionDriver(name, nil, nil, nil, io.Discard)
	newDriver := newSessionDriver(name, nil, nil, nil, io.Discard)
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
	approvalTab := newApprovalTable(io.Discard)
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
		newApprovalTable(io.Discard),
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
	h := &hub{warnings: io.Discard, teardownGrace: 5 * time.Second, live: map[string]liveSession{
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

func TestGoDeadReleasesTheSocketsWhileHoldingTheLifecycle(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	name := "lifecycle-" + newTurnID()
	h := newHub(
		state.NewState(filepath.Join(t.TempDir(), "state.json")),
		nil,
		nil,
		t.TempDir(),
		nil,
		nil,
		newApprovalTable(io.Discard),
		newSessionTable(),
	)
	h.runtime = layout
	t.Cleanup(func() { h.goDead(name) })

	if err := h.goLive(t.Context(), state.Session{Name: name, Incarnation: "incarnation"}); err != nil {
		t.Fatal(err)
	}
	probe := probeRelease(h, name)
	h.goDead(name)

	if got := probe.calls.Load(); got != 1 {
		t.Fatalf("goDead released the sockets %d times, want 1", got)
	}
	if probe.unlocked.Load() {
		t.Fatal("goDead released the sockets without holding the lifecycle lock, a concurrent goLive could still find the old socket answering")
	}
}

func TestRebindSurvivesAnOverrunTeardown(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	name := "overrun-" + newTurnID()
	sessionTab := newSessionTable()
	entered := make(chan struct{})
	holdTeardown := make(chan struct{})
	releaseTeardown := sync.OnceFunc(func() { close(holdTeardown) })
	var enterOnce sync.Once
	h := newHub(
		state.NewState(filepath.Join(t.TempDir(), "state.json")),
		nil,
		nil,
		t.TempDir(),
		nil,
		nil,
		newApprovalTable(io.Discard),
		sessionTab,
		withSessionStopped(func(string) {
			enterOnce.Do(func() { close(entered) })
			<-holdTeardown
		}),
	)
	h.runtime = layout
	h.teardownGrace = 0
	t.Cleanup(func() { h.goDead(name) })
	t.Cleanup(releaseTeardown)

	if err := h.goLive(t.Context(), state.Session{Name: name, Incarnation: "old-incarnation"}); err != nil {
		t.Fatal(err)
	}
	awaitRegisteredIncarnation(t, sessionTab, name, "old-incarnation")
	h.mu.Lock()
	oldDone := h.live[name].done
	h.mu.Unlock()
	probe := probeRelease(h, name)

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
	if got := probe.calls.Load(); got != 1 {
		t.Fatalf("goDead returned after %d releases of the overrun session, want 1", got)
	}
	if socketAnswers(t.Context(), layout, SessionCommandSocketPathIn(layout, h.instanceID, name)) {
		t.Fatal("the command socket still answers once goDead has returned")
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

type releaseProbe struct {
	calls    atomic.Int32
	unlocked atomic.Bool
}

func probeRelease(h *hub, name string) *releaseProbe {
	probe := &releaseProbe{}
	h.mu.Lock()
	live := h.live[name]
	h.mu.Unlock()
	original := live.release
	live.release = func() {
		probe.calls.Add(1)
		if h.lifecycle.TryLock() {
			h.lifecycle.Unlock()
			probe.unlocked.Store(true)
		}
		original()
	}
	h.mu.Lock()
	h.live[name] = live
	h.mu.Unlock()
	return probe
}

func withSessionStopped(fn func(name string)) hubOption {
	return func(h *hub) { h.sessionStopped = fn }
}
