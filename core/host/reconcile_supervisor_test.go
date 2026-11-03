package host

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestReconcileStopsTheBridgeOfARemovedSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	h := newHub(st, sup, nil, t.TempDir(), &cli.Registry{}, nil)

	sess := state.Session{Name: "orphan", ChannelID: "c1", Type: "text"}
	if err := st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	sup.Start(sess)
	h.reconcile(t.Context())
	if !sup.Running("orphan") {
		t.Fatal("a persisted session must keep its bridge")
	}

	if err := st.RemoveSession("orphan"); err != nil {
		t.Fatal(err)
	}
	h.reconcile(t.Context())
	if sup.Running("orphan") {
		t.Fatal("a removed session must not leave a supervised bridge behind")
	}
}

func TestReconcileStopsTheBridgeOfAnArchivedSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	h := newHub(st, sup, nil, t.TempDir(), &cli.Registry{}, nil)

	sess := state.Session{Name: "shelved", ChannelID: "c1", Type: "text"}
	if err := st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	sup.Start(sess)
	h.reconcile(t.Context())

	logCleanup(st.SetArchived("shelved", true))
	h.reconcile(t.Context())
	if sup.Running("shelved") {
		t.Fatal("an archived session must not leave a supervised bridge behind")
	}
}
