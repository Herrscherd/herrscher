package host

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func addRefusalSession(t *testing.T, h *hub, name string) {
	t.Helper()
	if err := h.st.AddSession(state.Session{Name: name, ChannelID: "c1", Incarnation: name}); err != nil {
		t.Fatal(err)
	}
}

func TestRefusedStartCreatesNoSocketAndStartsNoLoop(t *testing.T) {
	h := newTestHub(t)
	name := "refused-start-" + newTurnID()
	addRefusalSession(t, h, name)
	never := make(chan struct{})
	t.Cleanup(func() { close(never) })
	h.stopping = func(string) <-chan struct{} { return never }
	var listened, started atomic.Int32
	listen := h.listen
	h.listen = func(ctx context.Context, path string) (net.Listener, error) {
		listened.Add(1)
		return listen(ctx, path)
	}
	h.startBridge = func(state.Session, time.Duration) error {
		started.Add(1)
		return nil
	}
	sess, _ := h.st.FindSession(name)

	_, err := h.goLiveAndPersist(t.Context(), sess, func() error { return nil })
	if !errors.Is(err, supervisor.ErrStillStopping) {
		t.Fatalf("goLiveAndPersist = %v, want ErrStillStopping", err)
	}
	if n := listened.Load(); n != 0 {
		t.Fatalf("a refused start opened %d control socket(s), want none", n)
	}
	if n := started.Load(); n != 0 {
		t.Fatalf("a refused start launched %d bridge(s), want none", n)
	}
	if _, statErr := os.Stat(control.SocketPath(h.runtime, h.instanceID, name)); !os.IsNotExist(statErr) {
		t.Fatalf("a refused start left a control socket behind: %v", statErr)
	}
	if h.isLive(name) {
		t.Fatal("a refused start marked the session live")
	}
}

func TestRefusedSessionJournalsOnlyItsTransitions(t *testing.T) {
	h := newTestHub(t)
	name := "journal-" + newTurnID()
	addRefusalSession(t, h, name)
	var out bytes.Buffer
	h.warnings = &out
	var refusing atomic.Bool
	refusing.Store(true)
	h.startBridge = func(state.Session, time.Duration) error {
		if refusing.Load() {
			return supervisor.ErrStillStopping
		}
		return nil
	}

	for range 3 {
		h.reconcile(t.Context())
	}
	if n := strings.Count(out.String(), `"`+name+`"`); n != 1 {
		t.Fatalf("three refused commands printed %d lines for the session, want 1:\n%s", n, out.String())
	}

	refusing.Store(false)
	h.reconcile(t.Context())
	h.reconcile(t.Context())
	if n := strings.Count(out.String(), `"`+name+`"`); n != 2 {
		t.Fatalf("recovery and the commands after it printed %d lines for the session, want 2 (refusal then recovery):\n%s", n, out.String())
	}
	if !h.isLive(name) {
		t.Fatal("the session did not come live once its start stopped being refused")
	}
}
