package host

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const (
	drainQuietWindow = 100 * time.Millisecond
	drainWait        = 10 * time.Second
)

type recordingDrainer struct {
	called chan struct{}
}

func (d *recordingDrainer) DrainBridges() error {
	d.called <- struct{}{}
	return nil
}

func TestAwaitHubStopDrainsBridgesAfterSessionsStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	liveStopped := make(chan struct{})
	sockets := make(chan struct{})
	close(sockets)
	drainer := &recordingDrainer{called: make(chan struct{}, 4)}
	runningHub := testHub(&hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), runtime: testRuntime(t)})

	returned := make(chan error, 1)
	go func() {
		returned <- awaitHubStop(ctx, runningHub, drainer, liveStopped, sockets, slog.Default())
	}()

	cancel()
	select {
	case <-drainer.called:
		t.Fatal("DrainBridges ran while the sessions were still stopping")
	case <-time.After(drainQuietWindow):
	}

	close(liveStopped)
	select {
	case <-drainer.called:
	case <-time.After(drainWait):
		t.Fatal("DrainBridges was not called once the sessions stopped")
	}

	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("awaitHubStop returned %v, want context.Canceled", err)
		}
	case <-time.After(drainWait):
		t.Fatal("awaitHubStop did not return after shutdown")
	}

	select {
	case <-drainer.called:
		t.Fatal("DrainBridges called more than once at shutdown")
	default:
	}
}
