package host

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

type recordingDrainer struct {
	shutdown    <-chan struct{}
	liveStopped <-chan struct{}
	calls       int
	ctxDone     bool
	liveDone    bool
}

func (d *recordingDrainer) DrainBridges() error {
	d.calls++
	d.ctxDone = isClosed(d.shutdown)
	d.liveDone = isClosed(d.liveStopped)
	return nil
}

func TestAwaitHubStopDrainsBridgesAfterSessionsStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	liveStopped := make(chan struct{})
	sockets := make(chan struct{})
	close(sockets)
	drainer := &recordingDrainer{shutdown: ctx.Done(), liveStopped: liveStopped}
	runningHub := &hub{runtime: testRuntime(t)}

	returned := make(chan error, 1)
	go func() {
		returned <- awaitHubStop(ctx, runningHub, drainer, liveStopped, sockets, slog.Default())
	}()

	cancel()
	close(liveStopped)

	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("awaitHubStop returned %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("awaitHubStop did not return after shutdown")
	}
	if drainer.calls != 1 {
		t.Fatalf("DrainBridges called %d times at shutdown, want exactly once", drainer.calls)
	}
	if !drainer.ctxDone || !drainer.liveDone {
		t.Fatalf("DrainBridges ran before shutdown finished: ctx done = %v, sessions stopped = %v", drainer.ctxDone, drainer.liveDone)
	}
}
