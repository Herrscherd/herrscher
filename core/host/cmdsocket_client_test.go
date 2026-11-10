package host

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"
)

type clientDispatcher struct {
	out  string
	argv []string
}

func (d *clientDispatcher) Dispatch(_ context.Context, argv []string) (string, error) {
	d.argv = append([]string(nil), argv...)
	return d.out, nil
}

func TestDispatchLiveCommandRoundTrips(t *testing.T) {
	path := CommandSocketPath(fmt.Sprintf("client-test-%d-%d", os.Getpid(), time.Now().UnixNano()))
	ln, err := listenCommandSocket(t.Context(), isolatedRuntimeDir(t), path)
	if err != nil {
		t.Fatalf("listenCommandSocket: %v", err)
	}
	t.Cleanup(func() {
		logCleanup(ln.Close())
		logCleanup(os.Remove(path))
	})

	disp := &clientDispatcher{out: "worker created"}
	accepted := make(chan error, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr == nil {
			handleCommandConn(context.Background(), conn, disp, defaultCommandReadTimeout)
		}
		accepted <- acceptErr
	}()

	argv := []string{"session", "seed", "--name", "orchestrator", "--task", "read only"}
	out, handled, err := dispatchLiveCommand(context.Background(), isolatedRuntimeDir(t), path, argv)
	if err != nil {
		t.Fatalf("dispatchLiveCommand: %v", err)
	}
	if !handled {
		t.Fatal("dispatchLiveCommand handled = false, want true")
	}
	if out != disp.out {
		t.Fatalf("output = %q, want %q", out, disp.out)
	}
	if !slices.Equal(disp.argv, argv) {
		t.Fatalf("argv = %v, want %v", disp.argv, argv)
	}
	if err := <-accepted; err != nil {
		t.Fatalf("accept: %v", err)
	}
}

func TestDispatchLiveCommandUnavailableFallsBack(t *testing.T) {
	path := CommandSocketPath(fmt.Sprintf("missing-test-%d-%d", os.Getpid(), time.Now().UnixNano()))
	out, handled, err := dispatchLiveCommand(context.Background(), isolatedRuntimeDir(t), path, []string{"session", "seed"})
	if err != nil {
		t.Fatalf("unavailable command socket error = %v, want nil fallback", err)
	}
	if handled || out != "" {
		t.Fatalf("unavailable command socket = (%q, %v), want empty/false", out, handled)
	}
}
