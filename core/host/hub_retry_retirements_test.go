package host

import (
	"errors"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestRetryKeepsTheRetirementOfItsFirstAttempt(t *testing.T) {
	h := newTestHub(t)
	name := "retry-keeps-" + newTurnID()
	sess := state.Session{Name: name, ChannelID: "c1", Incarnation: name}
	if err := h.st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	h.startBridge = func(state.Session, time.Duration) error {
		attempts++
		if attempts == 1 {
			return supervisor.ErrStillStopping
		}
		return errBridgeRefusedStart
	}
	h.stopBridge = func(string) error { return nil }
	window, cancel := h.newGrace()
	defer cancel()

	retired, err := h.goLiveBridgedDeferred(t.Context(), sess, window)
	if attempts != 2 {
		t.Fatalf("startBridge ran %d times, want 2: the retry after a settled stop did not happen", attempts)
	}
	if !errors.Is(err, errBridgeRefusedStart) {
		t.Fatalf("goLiveBridgedDeferred = %v, want the retry's error %v", err, errBridgeRefusedStart)
	}
	if len(retired) != 2 {
		t.Fatalf("got %d retirements, want 2: the retry overwrote the retirement of the first attempt", len(retired))
	}
	for _, retiree := range retired {
		if retiree.name != name {
			t.Fatalf("retirement names %q, want %q", retiree.name, name)
		}
	}
	h.awaitRetired(window, retired)
	for i, retiree := range retired {
		select {
		case <-retiree.live.done:
		default:
			t.Fatalf("retirement %d was not joined: its session goroutine is still running", i)
		}
	}
}
