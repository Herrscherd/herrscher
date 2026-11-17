//go:build !windows

package host

import (
	"context"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"
)

type delayedDrainer struct{ hold time.Duration }

func (d delayedDrainer) DrainBridges() error {
	time.Sleep(d.hold)
	return nil
}

func TestStopDurationGrowsWithEachStuckPhase(t *testing.T) {
	window := 300 * time.Millisecond
	cases := []struct {
		name    string
		write   bool
		drain   bool
		command bool
		windows int
	}{
		{name: "nothing stuck", windows: 0},
		{name: "a stuck write", write: true, windows: 1},
		{name: "a stuck drain", drain: true, windows: 1},
		{name: "a stuck command", command: true, windows: 1},
		{name: "a stuck write then a stuck drain", write: true, drain: true, windows: 2},
		{name: "a stuck write and a stuck command overlap", write: true, command: true, windows: 1},
		{name: "a stuck drain and a stuck command overlap", drain: true, command: true, windows: 1},
		{name: "a stuck write, drain and command", write: true, drain: true, command: true, windows: 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			elapsed := stopDuration(t, window, c.write, c.command, c.drain)
			lower := time.Duration(c.windows) * window * 9 / 10
			upper := time.Duration(c.windows)*window*13/10 + window/3
			if elapsed < lower || elapsed > upper {
				t.Fatalf("the stop took %v, want between %v and %v: the sequential stuck windows are %d", elapsed, lower, upper, c.windows)
			}
		})
	}
}

func stopDuration(t *testing.T, window time.Duration, write, command, drain bool) time.Duration {
	t.Helper()
	name := "stop-" + newTurnID()
	h := newInflightHub(t, name, window, io.Discard)
	h.persists.grace = window
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if write {
		path := makeBlockingTranscript(t, h, name)
		startBlockedTranscriptWrite(t, h, name)
		t.Cleanup(func() { unblockTranscript(path) })
	}
	var released <-chan struct{}
	if command {
		released = startStuckCommand(ctx, t, h)
	} else {
		done := make(chan struct{})
		close(done)
		released = done
	}
	var drainHold time.Duration
	if drain {
		drainHold = window
	}

	liveStopped := make(chan struct{})
	go func() {
		<-ctx.Done()
		h.stopLive()
		close(liveStopped)
	}()
	started := time.Now()
	stopped := runInBackground(func() {
		logCleanup(awaitHubStop(ctx, h, delayedDrainer{hold: drainHold}, liveStopped, released, slog.New(slog.DiscardHandler)))
	})
	cancel()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the stop path never returned")
	}
	return time.Since(started)
}

func startStuckCommand(ctx context.Context, t *testing.T, h *hub) <-chan struct{} {
	t.Helper()
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	entered := make(chan struct{})
	disp := dispatcherFunc(func(context.Context, []string) (string, error) {
		close(entered)
		<-stuck
		return "late", nil
	})
	sockPath := filepath.Join(t.TempDir(), "cmd.sock")
	listener, err := openSocket(ctx, newHostMessages(), isolatedRuntimeDir(t), sockPath)
	if err != nil {
		t.Fatal(err)
	}
	_, released := acceptCommands(ctx, listener, disp, defaultCommandReadTimeout, h.teardownGrace, h.warnCommandHandlerOverrun)
	client, err := (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { logCleanup(client.Close()) })
	if _, err = client.Write([]byte(`{"argv":["stuck"]}` + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the stuck command never started")
	}
	return released
}
