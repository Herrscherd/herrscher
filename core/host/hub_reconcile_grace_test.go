package host

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

const reconcileGrace = 300 * time.Millisecond

func sessionListRegistry(t *testing.T) *cli.Registry {
	t.Helper()
	var reg cli.Registry
	list := contracts.New("session", "list").Do(func(context.Context, contracts.Input) (contracts.Result, error) {
		return contracts.DataResult("listed"), nil
	})
	if err := reg.Add(list); err != nil {
		t.Fatal(err)
	}
	return &reg
}

func TestStuckBridgesDoNotAddAGracePerSessionToAnUnrelatedCommand(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	for _, name := range []string{"stuck-a", "stuck-b", "stuck-c"} {
		if err := st.AddSession(state.Session{Name: name, ChannelID: "c1"}); err != nil {
			t.Fatal(err)
		}
	}
	h := newHub(st, nil, nil, t.TempDir(), sessionListRegistry(t), nil, newApprovalTable(io.Discard), newSessionTable())
	h.runtime = isolatedRuntimeDir(t)
	h.teardownGrace = reconcileGrace
	h.startBridge = func(_ state.Session, wait time.Duration) error {
		time.Sleep(wait)
		return supervisor.ErrStillStopping
	}
	h.stopBridge = func(string) error { return nil }
	t.Cleanup(h.stopLive)

	started := time.Now()
	out, err := cli.Plain(h.Dispatch(withLocalPrincipal(t.Context()), []string{"session", "list"}))
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Dispatch beside three stuck bridges: %v", err)
	}
	if out != "listed" {
		t.Fatalf("Dispatch output = %q, want %q", out, "listed")
	}
	if bound := reconcileGrace + 150*time.Millisecond; elapsed > bound {
		t.Fatalf("an unrelated command took %v beside three stuck bridges, want at most %v: each stuck start waited a grace", elapsed, bound)
	}
}

func TestStuckStopsOfArchivedSessionsShareOneGraceInReconcile(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	names := []string{"dead-a", "dead-b", "dead-c"}
	for _, name := range names {
		if err := st.AddSession(state.Session{Name: name, ChannelID: "c1", Incarnation: name}); err != nil {
			t.Fatal(err)
		}
	}
	h := newHub(st, nil, nil, t.TempDir(), sessionListRegistry(t), nil, newApprovalTable(io.Discard), newSessionTable())
	h.runtime = isolatedRuntimeDir(t)
	h.teardownGrace = reconcileGrace
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	h.stopBridge = func(string) error {
		<-release
		return nil
	}
	t.Cleanup(h.stopLive)
	for _, name := range names {
		sess, _ := st.FindSession(name)
		if _, err := h.goLiveAndPersist(t.Context(), sess, func() error { return nil }); err != nil {
			t.Fatalf("bring %s live: %v", name, err)
		}
		if err := st.SetArchived(name, true); err != nil {
			t.Fatal(err)
		}
	}

	started := time.Now()
	h.reconcile(t.Context())
	elapsed := time.Since(started)
	if bound := reconcileGrace + 150*time.Millisecond; elapsed > bound {
		t.Fatalf("reconcile took %v retiring three stuck bridges, want at most %v: each retirement waited its own grace", elapsed, bound)
	}
	for _, name := range names {
		if h.isLive(name) {
			t.Fatalf("archived session %s stayed live after reconcile", name)
		}
	}
}
