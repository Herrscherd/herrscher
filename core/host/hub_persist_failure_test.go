package host

import (
	"errors"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestFailedPersistOfALiveSessionJoinsItsGoroutine(t *testing.T) {
	h := newTestHub(t)
	name := "failed-persist-" + newTurnID()
	sess := state.Session{Name: name, ChannelID: "c1", Incarnation: name}
	errPersist := errors.New("persist failed")
	var done <-chan struct{}
	h.startBridge = func(state.Session, time.Duration) error {
		t.Error("startBridge ran although the persist of the session failed")
		return nil
	}
	h.stopBridge = func(string) error { return nil }

	_, err := h.goLiveAndPersist(t.Context(), sess, func() error {
		holdSessionExit(t, h.sessions, name)
		h.mu.Lock()
		done = h.live[name].done
		h.mu.Unlock()
		return errPersist
	})
	if !errors.Is(err, errPersist) {
		t.Fatalf("goLiveAndPersist = %v, want %v", err, errPersist)
	}
	if done == nil {
		t.Fatal("the session goroutine was not captured before the persist failed")
	}
	select {
	case <-done:
	default:
		t.Fatal("the session goroutine of a session whose persist failed was still running when goLiveAndPersist returned")
	}
	if h.isLive(name) {
		t.Fatal("a session whose persist failed stayed live")
	}
	if socketAnswers(t.Context(), h.runtime, SessionCommandSocketPathIn(h.runtime, h.instanceID, name)) {
		t.Fatal("the command socket of a session whose persist failed still answers once goLiveAndPersist has returned")
	}
}
