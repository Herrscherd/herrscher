package host

import (
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestReconcileReturnsOnlyAfterItsRetiredSessionExits(t *testing.T) {
	h := newTestHub(t)
	name := "reconcile-retired-" + newTurnID()
	sess := state.Session{Name: name, ChannelID: "c1", Incarnation: name}
	if err := h.st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	var done <-chan struct{}
	h.startBridge = func(state.Session, time.Duration) error {
		holdSessionExit(t, h.sessions, name)
		h.mu.Lock()
		done = h.live[name].done
		h.mu.Unlock()
		return errBridgeRefusedStart
	}
	h.stopBridge = func(string) error { return nil }

	if _, err := h.reconciling(t.Context(), func() (contracts.Result, error) {
		return contracts.Result{}, nil
	}); err != nil {
		t.Fatalf("reconciling = %v, want nil: a refused start belongs to reconcile, not to the command", err)
	}
	if done == nil {
		t.Fatal("the session goroutine was not captured before reconcile returned")
	}
	select {
	case <-done:
	default:
		t.Fatal("reconcile returned while the goroutine of a session it retired was still running")
	}
}
