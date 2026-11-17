package host

import (
	"context"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestCommandSocketReleasesWithinItsJoinBudgetWhenAHandlerNeverEnds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	entered := make(chan struct{})
	disp := dispatcherFunc(func(context.Context, []string) (string, error) {
		close(entered)
		<-stuck
		return "late", nil
	})
	listener, err := openSocket(ctx, isolatedRuntimeDir(t), path)
	if err != nil {
		t.Fatal(err)
	}
	var overruns atomic.Int32
	joinBudget := 200 * time.Millisecond
	released := acceptCommands(ctx, listener, disp, defaultCommandReadTimeout, joinBudget, func() { overruns.Add(1) })

	client, err := (&net.Dialer{}).DialContext(t.Context(), "unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { logCleanup(client.Close()) })
	if _, err := client.Write([]byte(`{"argv":["stuck"]}` + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never started")
	}

	started := time.Now()
	cancel()
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("the command socket never released while a handler was stuck")
	}
	if elapsed := time.Since(started); elapsed > joinBudget+time.Second {
		t.Fatalf("the command socket released after %v with a stuck handler, want about %v", elapsed, joinBudget)
	}
	if got := overruns.Load(); got != 1 {
		t.Fatalf("the stuck handler was announced %d times, want exactly once", got)
	}
	probe, err := (&net.Dialer{}).DialContext(t.Context(), "unix", path)
	if err == nil {
		logCleanup(probe.Close())
		t.Fatal("the command socket still answers after release")
	}
}
