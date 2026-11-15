package host

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestAFailedPersistLeavesNoSessionLiveAndHaltsItsBridge(t *testing.T) {
	h := newTestHub(t)
	name := "unsaved-" + newTurnID()
	sess := state.Session{Name: name, Incarnation: "only"}
	var launched atomic.Bool
	h.startBridge = func(state.Session, time.Duration) error {
		launched.Store(true)
		return nil
	}
	halted := make(chan string, 1)
	h.stopBridge = func(stopped string) error {
		halted <- stopped
		return nil
	}
	persistErr := errors.New("the state file is read-only")

	_, err := h.goLiveAndPersist(t.Context(), sess, func() error { return persistErr })
	if !errors.Is(err, persistErr) {
		t.Fatalf("goLiveAndPersist = %v, want the persist error", err)
	}
	if h.isLive(name) {
		t.Fatal("a session whose persist failed was left live")
	}
	if launched.Load() {
		t.Fatal("a bridge was launched for a session whose persist failed")
	}
	select {
	case got := <-halted:
		if got != name {
			t.Fatalf("halted bridge %q, want %q", got, name)
		}
	default:
		t.Fatal("the live session of a failed persist was not halted")
	}
}
