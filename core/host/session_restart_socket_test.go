package host

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestRestartOnTheSameNameReclaimsTheCommandSocket(t *testing.T) {
	name := "restart-" + newTurnID()
	h := newInflightHub(t, name, time.Minute, io.Discard)
	sess := state.Session{Name: name, ChannelID: "c1"}
	if err := h.goLive(t.Context(), sess); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	first := h.live[name]
	h.mu.Unlock()
	first.release()

	if err := h.goLive(t.Context(), sess); err != nil {
		t.Fatalf("the session could not come back on its own name right after its release: %v", err)
	}
	select {
	case <-first.released:
	case <-time.After(5 * time.Second):
		t.Fatal("the released command socket never finished its join")
	}

	probe, err := (&net.Dialer{}).DialContext(t.Context(), "unix", SessionCommandSocketPathIn(h.runtime, h.instanceID, name))
	if err != nil {
		t.Fatalf("the restarted command socket disappeared when the released one finished: %v", err)
	}
	logCleanup(probe.Close())
}
