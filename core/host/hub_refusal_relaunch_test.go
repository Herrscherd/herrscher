package host

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func addRefusalSession(t *testing.T, h *hub, name string) {
	t.Helper()
	if err := h.st.AddSession(state.Session{Name: name, ChannelID: "c1", Incarnation: name}); err != nil {
		t.Fatal(err)
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
