package host

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

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

func TestReconcileReapsInPlaceArchived(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	h := newHub(ctx, st, sup, nil, t.TempDir(), &cli.Registry{}, nil)

	if err := st.AddSession(state.Session{Name: "s", ChannelID: "c1", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	h.reconcile()
	h.mu.Lock()
	_, up := h.live["s"]
	h.mu.Unlock()
	if !up {
		t.Fatalf("session should be live before archiving")
	}

	logCleanup(st.SetArchived("s", true))
	h.reconcile()
	h.mu.Lock()
	_, stillUp := h.live["s"]
	h.mu.Unlock()
	if stillUp {
		t.Fatalf("archived session must be reaped from the live set on reconcile")
	}
}
