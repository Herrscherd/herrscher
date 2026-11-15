package host

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionArchivedWhileItsBridgeStartsIsNotLeftLive(t *testing.T) {
	h := newTestHub(t)
	name := "archived-mid-start-" + newTurnID()
	sess := state.Session{Name: name, ChannelID: "c1", Incarnation: name}
	if err := h.st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	var archiveErr error
	var done <-chan struct{}
	h.startBridge = func(state.Session, time.Duration) error {
		holdSessionExit(t, h.sessions, name)
		archiveErr = h.st.SetArchived(name, true)
		h.mu.Lock()
		done = h.live[name].done
		h.mu.Unlock()
		return nil
	}
	var halts atomic.Int32
	h.stopBridge = func(string) error {
		halts.Add(1)
		return nil
	}

	if err := h.goLiveBridged(t.Context(), sess); err != nil {
		t.Fatalf("goLiveBridged = %v", err)
	}
	if archiveErr != nil {
		t.Fatal(archiveErr)
	}
	if h.isLive(name) {
		t.Fatal("a session archived while its bridge started stayed live")
	}
	if got := halts.Load(); got != 1 {
		t.Fatalf("the bridge of the archived session was halted %d times, want 1", got)
	}
	if done == nil {
		t.Fatal("the session goroutine was not captured while its bridge started")
	}
	select {
	case <-done:
	default:
		t.Fatal("the session goroutine of an archived session was still running when goLiveBridged returned")
	}
	if socketAnswers(t.Context(), h.runtime, SessionCommandSocketPathIn(h.runtime, h.instanceID, name)) {
		t.Fatal("the command socket of an archived session still answers once goLiveBridged has returned")
	}
}
