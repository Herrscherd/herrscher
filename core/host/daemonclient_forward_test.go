package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

type errDispatcher struct{ err error }

func (d errDispatcher) Dispatch(context.Context, []string) (string, error) { return "", d.err }

func serveOneCommand(t *testing.T, layout RuntimeLayout, instance string, disp dispatcher) {
	t.Helper()
	path := CommandSocketPathIn(layout, instance)
	ln, err := listenCommandSocket(t.Context(), layout, path)
	if err != nil {
		t.Fatalf("listenCommandSocket: %v", err)
	}
	accepted := make(chan error, 1)
	t.Cleanup(func() {
		if err := <-accepted; err != nil {
			t.Errorf("accept: %v", err)
		}
	})
	t.Cleanup(func() {
		logCleanup(ln.Close())
		logCleanup(os.Remove(path))
	})
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			handleCommandConn(context.Background(), conn, disp, defaultCommandReadTimeout)
		}
		accepted <- err
	}()
}

func uniqueInstance() string {
	return fmt.Sprintf("fwd%d%d", os.Getpid(), time.Now().UnixNano()%1e6)
}

func TestForwardToDaemonReturnsTheDaemonsAnswer(t *testing.T) {
	instance := uniqueInstance()
	layout := testRuntime(t)
	disp := &clientDispatcher{out: "#general\n#random"}
	serveOneCommand(t, layout, instance, disp)

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.NewState(statePath)
	if err := st.SetInstanceID(instance); err != nil {
		t.Fatal(err)
	}

	argv := []string{"plugin-kind", "channel", "read", "--limit", "5"}
	out, handled, err := ForwardToDaemon(context.Background(), layout, statePath, "", argv)
	if err != nil {
		t.Fatalf("ForwardToDaemon: %v", err)
	}
	if !handled {
		t.Fatal("handled = false, want true with a daemon listening")
	}
	if out != disp.out {
		t.Fatalf("output = %q, want %q", out, disp.out)
	}
	if !slices.Equal(disp.argv, argv) {
		t.Fatalf("argv = %v, want %v unaltered", disp.argv, argv)
	}
}

func TestForwardToDaemonSurfacesTheDaemonsError(t *testing.T) {
	instance := uniqueInstance()
	layout := testRuntime(t)
	serveOneCommand(t, layout, instance, errDispatcher{err: errors.New(`unknown command "nope"`)})

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.NewState(statePath)
	if err := st.SetInstanceID(instance); err != nil {
		t.Fatal(err)
	}

	out, handled, err := ForwardToDaemon(context.Background(), layout, statePath, "", []string{"nope"})
	if !handled {
		t.Fatal("handled = false, want true: the daemon answered")
	}
	if err == nil {
		t.Fatal("error = nil, want the daemon's own refusal")
	}
	if err.Error() != `unknown command "nope"` {
		t.Fatalf("error = %q, want the daemon's wording", err)
	}
	if out != "" {
		t.Fatalf("output = %q, want empty on error", out)
	}
}

func TestForwardToDaemonReportsNoDaemon(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "absent.json")

	out, handled, err := ForwardToDaemon(context.Background(), testRuntime(t), statePath, uniqueInstance(), []string{"whatever"})
	if handled {
		t.Fatal("handled = true, want false: nothing is listening")
	}
	if err != nil {
		t.Fatalf("error = %v, want nil so the caller keeps its own answer", err)
	}
	if out != "" {
		t.Fatalf("output = %q, want empty", out)
	}
}
