package host

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestRefusalSettledBeforeItsRetryStartsTheBridgeAtOnce(t *testing.T) {
	h := newTestHub(t)
	name := "settled-before-retry-" + newTurnID()
	if err := h.st.AddSession(state.Session{Name: name, ChannelID: "c1", Incarnation: name}); err != nil {
		t.Fatal(err)
	}
	sess, _ := h.st.FindSession(name)
	settled := make(chan struct{})
	var stoppingChecks atomic.Int32
	h.stopping = func(string) <-chan struct{} {
		if stoppingChecks.Add(1) == 1 {
			return settled
		}
		return nil
	}
	var starts atomic.Int32
	h.startBridge = func(state.Session, time.Duration) error {
		starts.Add(1)
		return nil
	}
	h.stopBridge = func(string) error { return nil }

	if err := h.goLiveBridged(t.Context(), sess); err != nil {
		t.Fatalf("a start refused by a stop that settled meanwhile = %v, want it to start at once", err)
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("the bridge was started %d times after its stop settled, want 1", got)
	}
	if !h.isLive(name) {
		t.Fatal("the session whose stop settled was not live after its retry")
	}
	close(settled)
}

func TestRefusalWithNoStopInProgressIsNotRetriedInALoop(t *testing.T) {
	h := newTestHub(t)
	name := "refused-no-stop-" + newTurnID()
	if err := h.st.AddSession(state.Session{Name: name, ChannelID: "c1", Incarnation: name}); err != nil {
		t.Fatal(err)
	}
	sess, _ := h.st.FindSession(name)
	var starts atomic.Int32
	h.startBridge = func(state.Session, time.Duration) error {
		starts.Add(1)
		return supervisor.ErrStillStopping
	}
	h.stopBridge = func(string) error { return nil }

	if err := h.goLiveBridged(t.Context(), sess); !errors.Is(err, supervisor.ErrStillStopping) {
		t.Fatalf("goLiveBridged = %v, want ErrStillStopping", err)
	}
	if got := starts.Load(); got != 2 {
		t.Fatalf("a refusal with no stop in progress tried the start %d times, want exactly 2 (one retry)", got)
	}
}
