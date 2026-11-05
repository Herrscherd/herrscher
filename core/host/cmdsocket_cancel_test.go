package host

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestDispatchLiveCommandReturnsWhenItsContextIsCancelled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmd.sock")
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logCleanup(listener.Close()) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, _, dispatchErr := dispatchLiveCommand(ctx, path, []string{"status"})
		result <- dispatchErr
	}()

	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logCleanup(conn.Close()) })

	cancel()
	select {
	case dispatchErr := <-result:
		if dispatchErr == nil {
			t.Fatal("a cancelled dispatch must report the cancellation")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("dispatchLiveCommand stayed blocked on a silent daemon after its context was cancelled")
	}
}

func TestDispatchLiveCommandReportsCancellationBeforeDial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmd.sock")
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logCleanup(listener.Close()) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, dispatchErr := dispatchLiveCommand(ctx, path, []string{"status"})
	if !errors.Is(dispatchErr, context.Canceled) {
		t.Fatalf("dispatchLiveCommand error = %v, want context.Canceled", dispatchErr)
	}
}
