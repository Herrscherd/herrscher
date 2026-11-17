//go:build !windows

package host

import (
	"context"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/supervisor"
	"github.com/Herrscherd/herrscher/core/service"
)

type instantDrainer struct{}

func (instantDrainer) DrainBridges() error { return nil }

func TestStopPathStaysWithinItsBoundWithAStuckWriteAndAStuckCommand(t *testing.T) {
	grace := 200 * time.Millisecond
	name := "persist-" + newTurnID()
	warnings := &lockedBuffer{}
	h := newInflightHub(t, name, grace, warnings)
	path := makeBlockingTranscript(t, h, name)
	startBlockedTranscriptWrite(t, h, name)
	t.Cleanup(func() { unblockTranscript(path) })

	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	entered := make(chan struct{})
	disp := dispatcherFunc(func(context.Context, []string) (string, error) {
		close(entered)
		<-stuck
		return "late", nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
	if _, err := client.Write([]byte(`{"argv":["stuck"]}` + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the stuck command never started")
	}

	liveStopped := make(chan struct{})
	go func() {
		<-ctx.Done()
		h.stopLive()
		close(liveStopped)
	}()
	started := time.Now()
	stopped := runInBackground(func() {
		logCleanup(awaitHubStop(ctx, h, instantDrainer{}, liveStopped, released, slog.New(slog.DiscardHandler)))
	})
	cancel()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the stop path never returned with a stuck write and a stuck command")
	}

	bound := 6*grace + persistWriteGrace + supervisor.DrainBound
	if elapsed := time.Since(started); elapsed > bound {
		t.Fatalf("the stop path took %v with a stuck write and a stuck command, want under %v", elapsed, bound)
	}
	realBound := 6*defaultTeardownGrace + persistWriteGrace + supervisor.DrainBound
	if floor := realBound + service.StopMargin; service.StopTimeout < floor {
		t.Fatalf("StopTimeout is %v, want at least the stop bound %v plus the %v margin, %v", service.StopTimeout, realBound, service.StopMargin, floor)
	}
	if got := strings.Count(warnings.String(), persistOverrunText(h)); got != 1 {
		t.Fatalf("the persist overrun was announced %d times, want exactly once: %q", got, warnings.String())
	}
	if got := strings.Count(warnings.String(), commandOverrunText(h, grace)); got != 1 {
		t.Fatalf("the stuck command was announced %d times, want exactly once: %q", got, warnings.String())
	}
	probe, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
	if err == nil {
		logCleanup(probe.Close())
		t.Fatal("the command socket still answers after the stop")
	}
}

func commandOverrunText(h *hub, grace time.Duration) string {
	return newHostMessages().text(h.textLanguage()(), "warn.command_handler_overrun", map[string]any{graceParam: grace.String()})
}
