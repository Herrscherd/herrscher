package host

import (
	"errors"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

var errBridgeRefusedStart = errors.New("the bridge refused the start")

func TestRefusedStartJoinsTheSessionGoroutine(t *testing.T) {
	h := newTestHub(t)
	name := "refused-start-" + newTurnID()
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

	if err := h.goLiveBridged(t.Context(), sess); !errors.Is(err, errBridgeRefusedStart) {
		t.Fatalf("goLiveBridged = %v, want %v", err, errBridgeRefusedStart)
	}
	if done == nil {
		t.Fatal("the session goroutine was not captured before the start was refused")
	}
	select {
	case <-done:
	default:
		t.Fatal("the session goroutine of a refused start was still running when goLiveBridged returned")
	}
	if h.isLive(name) {
		t.Fatal("a session whose start was refused stayed live")
	}
}
