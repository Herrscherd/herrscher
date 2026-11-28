package host

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
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
	reg := cli.NewRegistry(clitext.NewMessages())
	list := contracts.New("session", "list").Do(func(context.Context, contracts.Input) (contracts.Result, error) {
		return contracts.DataResult("listed"), nil
	})
	if err := reg.Add(list); err != nil {
		t.Fatal(err)
	}
	h := newHub(st, nil, nil, t.TempDir(), reg, nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages(), clitext.NewMessages(), nil, nil))
	h.runtime = layout
	h.teardownGrace = time.Hour
	settling := make(chan struct{})
	var settled atomic.Bool
	h.stopping = func(name string) <-chan struct{} {
		if name == stuck && !settled.Load() {
			return settling
		}
		return nil
	}
	h.stopBridge = func(string) error { return nil }
	t.Cleanup(func() {
		h.goDead(stuck)
		h.goDead(other)
	})
	t.Cleanup(func() {
		settled.Store(true)
		close(settling)
		awaitBackground(t, h.relaunches.settled(), "the relaunch of the refused session outlived the test")
	})

	var resumeErr error
	resumed := runInBackground(func() { resumeErr = h.Resume(stuck) })
	awaitBackground(t, resumed, "Resume hung on the stopping bridge of the refused session")
	if !errors.Is(resumeErr, supervisor.ErrStillStopping) {
		t.Fatalf("Resume = %v, want ErrStillStopping", resumeErr)
	}

	var out string
	var dispatchErr error
	dispatched := runInBackground(func() {
		out, dispatchErr = cli.Plain(h.Dispatch(withLocalPrincipal(t.Context()), []string{"session", "list"}))
	})
	awaitBackground(t, dispatched, "Dispatch waited on the stopping bridge of the refused session")

	if dispatchErr != nil {
		t.Fatalf("Dispatch during a refused resume: %v", dispatchErr)
	}
	if out != "listed" {
		t.Fatalf("Dispatch output = %q during a refused resume, want %q", out, "listed")
	}
	if !h.isLive(other) {
		t.Fatal("the other session was not brought live by the dispatch")
	}
	if h.isLive(stuck) {
		t.Fatal("the refused session was marked live")
	}
}
