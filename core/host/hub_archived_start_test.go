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
	if err := h.st.AddSession(state.Session{Name: name, ChannelID: "c1", Incarnation: name}); err != nil {
		t.Fatal(err)
	}
	var archiveErr error
	h.startBridge = func(state.Session, time.Duration) error {
		archiveErr = h.st.SetArchived(name, true)
		return nil
	}
	var halts atomic.Int32
	h.stopBridge = func(string) error {
		halts.Add(1)
		return nil
	}

	if err := h.reviveIfActive(t.Context(), name); err != nil {
		t.Fatalf("reviveIfActive = %v", err)
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
}
