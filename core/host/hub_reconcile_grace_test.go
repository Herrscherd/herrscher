package host

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
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
	synctest.Test(t, func(t *testing.T) {
		st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
		names := []string{"stuck-a", "stuck-b", "stuck-c"}
		h := newHub(st, nil, nil, t.TempDir(), sessionListRegistry(t), nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages(), clitext.NewMessages()))
		h.teardownGrace = reconcileGrace
		release := make(chan struct{})
		defer close(release)
		h.stopBridge = func(string) error {
			<-release
			return nil
		}
		for _, name := range names {
			if err := st.AddSession(state.Session{Name: name, ChannelID: "c1", Incarnation: name}); err != nil {
				t.Fatal(err)
			}
			if err := st.SetArchived(name, true); err != nil {
				t.Fatal(err)
			}
			finished := make(chan struct{})
			close(finished)
			h.live[name] = liveSession{cancel: func() {}, done: finished, release: func() {}}
		}

		started := time.Now()
		out, err := cli.Plain(h.Dispatch(withLocalPrincipal(t.Context()), []string{"session", "list"}))
		elapsed := time.Since(started)
		if err != nil {
			t.Fatalf("Dispatch beside three stuck bridges: %v", err)
		}
		if out != "listed" {
			t.Fatalf("Dispatch output = %q, want %q", out, "listed")
		}
		if bound := reconcileGrace + reconcileGrace/2; elapsed > bound {
			t.Fatalf("an unrelated command took %v beside three stuck bridges, want at most %v: each stuck bridge waited its own grace", elapsed, bound)
		}
	})
}

func TestStuckStopsOfArchivedSessionsShareOneGraceInReconcile(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
		names := []string{"dead-a", "dead-b", "dead-c"}
		h := newHub(st, nil, nil, t.TempDir(), sessionListRegistry(t), nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages(), clitext.NewMessages()))
		h.teardownGrace = reconcileGrace
		release := make(chan struct{})
		defer close(release)
		h.stopBridge = func(string) error {
			<-release
			return nil
		}
		for _, name := range names {
			if err := st.AddSession(state.Session{Name: name, ChannelID: "c1", Incarnation: name}); err != nil {
				t.Fatal(err)
			}
			if err := st.SetArchived(name, true); err != nil {
				t.Fatal(err)
			}
			finished := make(chan struct{})
			close(finished)
			h.live[name] = liveSession{cancel: func() {}, done: finished, release: func() {}}
		}

		started := time.Now()
		h.reconcile(t.Context())
		elapsed := time.Since(started)
		if bound := reconcileGrace + reconcileGrace/2; elapsed > bound {
			t.Fatalf("reconcile took %v retiring three stuck bridges, want at most %v: each retirement waited its own grace", elapsed, bound)
		}
		for _, name := range names {
			if h.isLive(name) {
				t.Fatalf("archived session %s stayed live after reconcile", name)
			}
		}
	})
}
