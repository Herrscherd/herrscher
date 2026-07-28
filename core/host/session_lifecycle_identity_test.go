package host

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestGoDeadWaitsForRunSessionTeardown(t *testing.T) {
	cancelled := make(chan struct{})
	tornDown := make(chan struct{})
	returned := make(chan struct{})
	h := &hub{live: map[string]liveSession{
		"same-name": {
			cancel: func() { close(cancelled) },
			done:   tornDown,
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
	name := "registry-" + newTurnID()
	oldDriver := newSessionDriver(name, nil, nil, nil)
	newDriver := newSessionDriver(name, nil, nil, nil)
	registerDriver(name, oldDriver)
	t.Cleanup(func() { unregisterDriver(name, newDriver) })

	registerDriver(name, newDriver)
	unregisterDriver(name, oldDriver)

	sessionRegistry.mu.Lock()
	got := sessionRegistry.m[name]
	sessionRegistry.mu.Unlock()
	if got != newDriver {
		t.Fatalf("old teardown removed replacement driver: got %p want %p", got, newDriver)
	}
}

func TestSameNameSessionCanRebindAfterTeardown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	name := "rebind-" + newTurnID()
	h := newHub(
		ctx,
		state.NewState(filepath.Join(t.TempDir(), "state.json")),
		nil,
		nil,
		t.TempDir(),
		nil,
		nil,
	)
	t.Cleanup(func() { h.goDead(name) })

	h.goLive(state.Session{Name: name, Incarnation: "old-incarnation"})
	waitForRegisteredIncarnation(t, name, "old-incarnation")
	h.goDead(name)

	h.goLive(state.Session{Name: name, Incarnation: "new-incarnation"})
	waitForRegisteredIncarnation(t, name, "new-incarnation")
	conn, err := control.Dial(control.SocketPath(name))
	if err != nil {
		t.Fatalf("replacement control listener is not reachable: %v", err)
	}
	_ = conn.Close()
}

func waitForRegisteredIncarnation(t *testing.T, name, incarnation string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		sessionRegistry.mu.Lock()
		driver := sessionRegistry.m[name]
		sessionRegistry.mu.Unlock()
		if driver != nil && driver.incarnation == incarnation {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %q did not register incarnation %q", name, incarnation)
		}
		time.Sleep(time.Millisecond)
	}
}
