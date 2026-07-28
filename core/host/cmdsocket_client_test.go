package host

import (
	"context"
	"fmt"
	"os"
	"reflect"
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
	ln, err := listenCommandSocket(path)
	if err != nil {
		t.Fatalf("listenCommandSocket: %v", err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
		_ = os.Remove(path)
	})

	disp := &clientDispatcher{out: "worker created"}
	accepted := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			handleCommandConn(context.Background(), conn, disp, defaultCommandReadTimeout)
		}
		accepted <- err
	}()

	argv := []string{"session", "seed", "--name", "orchestrator", "--task", "read only"}
	out, handled, err := dispatchLiveCommand(context.Background(), path, argv)
	if err != nil {
		t.Fatalf("dispatchLiveCommand: %v", err)
	}
	if !handled {
		t.Fatal("dispatchLiveCommand handled = false, want true")
	}
	if out != disp.out {
		t.Fatalf("output = %q, want %q", out, disp.out)
	}
	if !reflect.DeepEqual(disp.argv, argv) {
		t.Fatalf("argv = %v, want %v", disp.argv, argv)
	}
	if err := <-accepted; err != nil {
		t.Fatalf("accept: %v", err)
	}
}

func TestDispatchLiveCommandUnavailableFallsBack(t *testing.T) {
	path := CommandSocketPath(fmt.Sprintf("missing-test-%d-%d", os.Getpid(), time.Now().UnixNano()))
	out, handled, err := dispatchLiveCommand(context.Background(), path, []string{"session", "seed"})
	if err != nil {
		t.Fatalf("unavailable command socket error = %v, want nil fallback", err)
	}
	if handled || out != "" {
		t.Fatalf("unavailable command socket = (%q, %v), want empty/false", out, handled)
	}
}
