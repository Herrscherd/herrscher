package host

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

// reconcile brings live sessions up but must leave archived ones untouched —
// else every old session respawns a backend at startup.
func TestReconcileSkipsArchived(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	h := newHub(ctx, st, sup, nil, t.TempDir(), &cli.Registry{}, nil)

	if err := st.AddSession(state.Session{Name: "recon-live", ChannelID: "c1", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "recon-arch", ChannelID: "c2", Type: "text", Archived: true}); err != nil {
		t.Fatal(err)
	}
	h.reconcile()

	h.mu.Lock()
	_, liveUp := h.live["recon-live"]
	_, archUp := h.live["recon-arch"]
	h.mu.Unlock()
	if !liveUp {
		t.Fatalf("live session should be supervised")
	}
	if archUp {
		t.Fatalf("archived session must not be supervised")
	}
}
