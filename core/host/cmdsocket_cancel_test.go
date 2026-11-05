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
	_, handled, dispatchErr := dispatchLiveCommand(ctx, path, []string{"status"})
	if !handled {
		t.Fatal("a cancelled dial must stay handled, or the caller would run the command locally")
	}
	if !errors.Is(dispatchErr, context.Canceled) {
		t.Fatalf("dispatchLiveCommand error = %v, want context.Canceled", dispatchErr)
	}
}

func TestCancellationOrPrefersTheContextError(t *testing.T) {
	broken := errors.New("write: broken pipe")
	ctx, cancel := context.WithCancel(context.Background())
	if got := cancellationOr(ctx, broken); !errors.Is(got, broken) {
		t.Fatalf("live context: got %v, want the transport error", got)
	}
	cancel()
	if got := cancellationOr(ctx, broken); !errors.Is(got, context.Canceled) {
		t.Fatalf("cancelled context: got %v, want context.Canceled", got)
	}
}
