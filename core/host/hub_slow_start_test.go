package host

import (
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSlowStartCannotResurrectAStoppedSession(t *testing.T) {
	h := newTestHub(t)
	h.teardownGrace = 20 * time.Millisecond
	name := "slow-start-" + newTurnID()
	sess := state.Session{Name: name, Incarnation: "only"}

	entered := make(chan struct{})
	enteredOnce := sync.OnceFunc(func() { close(entered) })
	release := make(chan struct{})
	releaseStart := sync.OnceFunc(func() { close(release) })
	started := make(chan struct{})
	realStart := h.startBridge
	h.startBridge = func(bridged state.Session, grace time.Duration) error {
		enteredOnce()
		<-release
		err := realStart(bridged, grace)
		close(started)
		return err
	}
	t.Cleanup(func() { h.goDead(name) })
	t.Cleanup(releaseStart)

	persisted := make(chan error, 1)
	go func() {
		_, err := h.goLiveAndPersist(t.Context(), sess, func() error { return nil })
		persisted <- err
	}()
	awaitClosed(t, entered, "the bridge start was never reached")

	dropped := make(chan struct{})
	go func() {
		h.goDead(name)
		close(dropped)
	}()
	time.Sleep(100 * time.Millisecond)
	releaseStart()

	awaitClosed(t, started, "the bridge start never completed")
	select {
	case err := <-persisted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("goLiveAndPersist never returned")
	}
	awaitClosed(t, dropped, "goDead never returned")
	if h.sup.Running(name) {
		t.Fatal("a bridge start that outlived the grace resurrected a session goDead had removed")
	}
}

func awaitClosed(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal(msg)
	}
}
