package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// errDispatcher stands in for a daemon whose registry rejects the argv.
type errDispatcher struct{ err error }

func (d errDispatcher) Dispatch(context.Context, []string) (string, error) { return "", d.err }

// serveOneCommand answers exactly one request on the instance's command socket,
// which is all a single forwarded argv needs. TMPDIR is redirected first so the
// socket lands in the test's own temp dir rather than the machine's.
func serveOneCommand(t *testing.T, instance string, disp dispatcher) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	path := CommandSocketPath(instance)
	ln, err := listenCommandSocket(path)
	if err != nil {
		t.Fatalf("listenCommandSocket: %v", err)
	}
	// The accept error is channelled back and joined, so a listener that never
	// took the connection fails as itself instead of as handled = false. Joining
	// is registered first so it runs after the close below, which is what frees a
	// goroutine still parked in Accept.
	accepted := make(chan error, 1)
	t.Cleanup(func() {
		if err := <-accepted; err != nil {
			t.Errorf("accept: %v", err)
		}
	})
	t.Cleanup(func() {
		_ = ln.Close()
		_ = os.Remove(path)
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
	disp := &clientDispatcher{out: "#general\n#random"}
	serveOneCommand(t, instance, disp)

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.NewState(statePath)
	if err := st.SetInstanceID(instance); err != nil {
		t.Fatal(err)
	}

	// A verb this process has no case for: it is relayed byte for byte, because
	// only the daemon's registry knows what it means.
	argv := []string{"plugin-kind", "channel", "read", "--limit", "5"}
	out, handled, err := ForwardToDaemon(context.Background(), statePath, "", argv)
	if err != nil {
		t.Fatalf("ForwardToDaemon: %v", err)
	}
	if !handled {
		t.Fatal("handled = false, want true with a daemon listening")
	}
	if out != disp.out {
		t.Fatalf("output = %q, want %q", out, disp.out)
	}
	if !reflect.DeepEqual(disp.argv, argv) {
		t.Fatalf("argv = %v, want %v unaltered", disp.argv, argv)
	}
}

func TestForwardToDaemonSurfacesTheDaemonsError(t *testing.T) {
	instance := uniqueInstance()
	serveOneCommand(t, instance, errDispatcher{err: errors.New(`unknown command "nope"`)})

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.NewState(statePath)
	if err := st.SetInstanceID(instance); err != nil {
		t.Fatal(err)
	}

	out, handled, err := ForwardToDaemon(context.Background(), statePath, "", []string{"nope"})
	if !handled {
		t.Fatal("handled = false, want true: the daemon answered")
	}
	if err == nil {
		t.Fatal("error = nil, want the daemon's own refusal")
	}
	// Verbatim: a vaguer message would hide which verb the daemon rejected.
	if err.Error() != `unknown command "nope"` {
		t.Fatalf("error = %q, want the daemon's wording", err)
	}
	if out != "" {
		t.Fatalf("output = %q, want empty on error", out)
	}
}

func TestForwardToDaemonReportsNoDaemon(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	statePath := filepath.Join(t.TempDir(), "absent.json")

	out, handled, err := ForwardToDaemon(context.Background(), statePath, uniqueInstance(), []string{"whatever"})
	if handled {
		t.Fatal("handled = true, want false: nothing is listening")
	}
	// Not an error: the caller decides what an unreachable daemon means, and for
	// the CLI it means the verb simply does not exist here.
	if err != nil {
		t.Fatalf("error = %v, want nil so the caller keeps its own answer", err)
	}
	if out != "" {
		t.Fatalf("output = %q, want empty", out)
	}
}
