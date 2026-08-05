package host

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

// A session removed from state must lose its bridge, not just its socket. The
// supervisor's restart loop holds its session by value and respawns forever, so
// a bridge left running after its socket is gone dials a path that no longer
// exists and crash-loops until the daemon is restarted — which is exactly the
// orphan process this reconcile step exists to prevent.
func TestReconcileStopsTheBridgeOfARemovedSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	h := newHub(ctx, st, sup, nil, t.TempDir(), &cli.Registry{}, nil)

	sess := state.Session{Name: "orphan", ChannelID: "c1", Type: "text"}
	if err := st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	if err := sup.Start(sess); err != nil {
		t.Fatal(err)
	}
	h.reconcile()
	if !sup.Running("orphan") {
		t.Fatal("a persisted session must keep its bridge")
	}

	if err := st.RemoveSession("orphan"); err != nil {
		t.Fatal(err)
	}
	h.reconcile()
	if sup.Running("orphan") {
		t.Fatal("a removed session must not leave a supervised bridge behind")
	}
}

// Archiving in place stops the bridge too: an archived session is revived only
// through Resume, which starts a fresh one.
func TestReconcileStopsTheBridgeOfAnArchivedSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	h := newHub(ctx, st, sup, nil, t.TempDir(), &cli.Registry{}, nil)

	sess := state.Session{Name: "shelved", ChannelID: "c1", Type: "text"}
	if err := st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	if err := sup.Start(sess); err != nil {
		t.Fatal(err)
	}
	h.reconcile()

	_ = st.SetArchived("shelved", true)
	h.reconcile()
	if sup.Running("shelved") {
		t.Fatal("an archived session must not leave a supervised bridge behind")
	}
}
