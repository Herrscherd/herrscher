package host

import (
	"errors"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestStillStoppingStartLeavesTheSessionDownAndTheLifecycleFree(t *testing.T) {
	h := newTestHub(t)
	name := "still-stopping-" + newTurnID()
	sess := state.Session{Name: name, Incarnation: "only"}
	h.startBridge = func(state.Session, time.Duration) error { return supervisor.ErrStillStopping }
	t.Cleanup(func() { h.goDead(name) })

	_, err := h.goLiveAndPersist(t.Context(), sess, func() error { return nil })
	if !errors.Is(err, supervisor.ErrStillStopping) {
		t.Fatalf("goLiveAndPersist = %v, want ErrStillStopping", err)
	}
	if h.isLive(name) {
		t.Fatal("a session whose bridge did not start was marked live")
	}
	if !h.lifecycle.TryLock() {
		t.Fatal("the lifecycle stayed held after a refused start")
	}
	h.lifecycle.Unlock()
}
