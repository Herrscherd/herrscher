package host

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestDispatchOfAnotherSessionReturnsWhileAResumeIsRefused(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	stuck := "refused-" + newTurnID()
	other := "served-" + newTurnID()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	for _, name := range []string{stuck, other} {
		if err := st.AddSession(state.Session{Name: name, ChannelID: "c1"}); err != nil {
			t.Fatal(err)
		}
	}
	var reg cli.Registry
	list := contracts.New("session", "list").Do(func(context.Context, contracts.Input) (contracts.Result, error) {
		return contracts.DataResult("listed"), nil
	})
	if err := reg.Add(list); err != nil {
		t.Fatal(err)
	}
	h := newHub(st, nil, nil, t.TempDir(), &reg, nil, newApprovalTable(io.Discard), newSessionTable())
	h.runtime = layout
	const grace = 300 * time.Millisecond
	h.teardownGrace = grace
	h.startBridge = func(sess state.Session, wait time.Duration) error {
		if sess.Name != stuck {
			return nil
		}
		time.Sleep(wait)
		return supervisor.ErrStillStopping
	}
	h.stopBridge = func(string) error { return nil }
	t.Cleanup(func() {
		h.goDead(stuck)
		h.goDead(other)
	})

	if err := h.Resume(stuck); !errors.Is(err, supervisor.ErrStillStopping) {
		t.Fatalf("Resume = %v, want ErrStillStopping", err)
	}

	started := time.Now()
	out, err := cli.Plain(h.Dispatch(withLocalPrincipal(t.Context()), []string{"session", "list"}))
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Dispatch during a refused resume: %v", err)
	}
	if out != "listed" {
		t.Fatalf("Dispatch output = %q during a refused resume, want %q", out, "listed")
	}
	if bound := grace + 150*time.Millisecond; elapsed > bound {
		t.Fatalf("Dispatch took %v during a refused resume, want it bounded by the grace %v plus a margin (%v)", elapsed, grace, bound)
	}
	if !h.isLive(other) {
		t.Fatal("the other session was not brought live by the dispatch")
	}
	if h.isLive(stuck) {
		t.Fatal("the refused session was marked live")
	}
}
