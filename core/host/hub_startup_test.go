package host

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

type closableObserver struct {
	t      *testing.T
	closed atomic.Bool
}

func (o *closableObserver) Write(p []byte) (int, error) {
	if o.closed.Load() {
		o.t.Error("a startup goroutine wrote to the observer after stopLive returned")
	}
	return len(p), nil
}

func newStartupTestHub(t *testing.T) *hub {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher", testRuntime(t))
	return newHub(st, sup, nil, t.TempDir(), &cli.Registry{}, nil, newApprovalTable(io.Discard), newSessionTable())
}

func TestStopLiveJoinsAStartupItNoLongerTracksAsLive(t *testing.T) {
	h := newStartupTestHub(t)
	observer := &closableObserver{t: t}
	startCtx, startCancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() {
		startCancel()
		<-done
	})
	h.trackStartup(startupRun{name: "retired", cancel: startCancel, done: done})
	go func() {
		defer close(done)
		<-startCtx.Done()
		if _, err := observer.Write([]byte("stopped")); err != nil {
			t.Error(err)
		}
	}()

	h.stopLive()
	observer.closed.Store(true)

	select {
	case <-done:
	default:
		t.Fatal("stopLive returned while a startup goroutine was still running")
	}
}

func TestStopLiveDoesNotHoldOnAStartupThatNeverEnds(t *testing.T) {
	h := newStartupTestHub(t)
	h.teardownGrace = 50 * time.Millisecond
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	h.trackStartup(startupRun{name: "blocked", cancel: func() {}, done: stuck})

	started := time.Now()
	h.stopLive()

	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("stopLive held on a blocked startup for %v, want the teardown grace bound", elapsed)
	}
}
