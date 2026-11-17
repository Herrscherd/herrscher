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

	"github.com/Herrscherd/herrscher/core/internal/supervisor"
	"github.com/Herrscherd/herrscher/core/service"
)

type delayedDrainer struct{ hold time.Duration }

func (d delayedDrainer) DrainBridges() error {
	time.Sleep(d.hold)
	return nil
}

func TestStopDurationGrowsWithEachStuckPhase(t *testing.T) {
	grace := 200 * time.Millisecond
	scale := func(full time.Duration) time.Duration {
		return full * grace / service.StopTeardownGrace
	}
	persist := scale(service.StopPersistGrace)
	drain := scale(supervisor.DrainBound)
	tolerance := grace / 4
	cases := []struct {
		name      string
		write     bool
		command   bool
		drainHold time.Duration
		want      time.Duration
	}{
		{name: "nothing stuck", want: 0},
		{name: "a stuck write", write: true, want: persist},
		{name: "a stuck drain", drainHold: drain, want: drain},
		{name: "a stuck write then a stuck drain", write: true, drainHold: drain, want: persist + drain},
		{name: "a stuck command", command: true, want: grace},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			elapsed := stopDuration(t, grace, persist, c.write, c.command, c.drainHold)
			if elapsed < c.want-tolerance || elapsed > c.want+tolerance {
				t.Fatalf("the stop took %v, want %v within %v: a phase was added, dropped or no longer bounded", elapsed, c.want, tolerance)
			}
		})
	}
}

func stopDuration(t *testing.T, grace, persist time.Duration, write, command bool, drainHold time.Duration) time.Duration {
	t.Helper()
	name := "stop-" + newTurnID()
	h := newInflightHub(t, name, grace, io.Discard)
	h.persists.grace = persist
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if write {
		path := makeBlockingTranscript(t, h, name)
		startBlockedTranscriptWrite(t, h, name)
		t.Cleanup(func() { unblockTranscript(path) })
	}
	released := make(chan struct{})
	close(released)
	if command {
		released = startStuckCommand(ctx, t, h)
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
	listener, err := openSocket(ctx, isolatedRuntimeDir(t), sockPath)
	if err != nil {
		t.Fatal(err)
	}
	released := acceptCommands(ctx, listener, disp, defaultCommandReadTimeout, h.teardownGrace, h.warnCommandHandlerOverrun)
	client, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
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
