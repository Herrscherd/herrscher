package host

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestServedInstanceIDPrefersTheFrozenID(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	st := state.NewState(p)
	if err := st.SetInstanceID("u03787009"); err != nil {
		t.Fatal(err)
	}
	if got := ServedInstanceID(p, ""); got != "u03787009" {
		t.Fatalf("ServedInstanceID = %q, want the id the daemon froze", got)
	}
	if got := ServedInstanceID(p, "something-else"); got != "u03787009" {
		t.Fatalf("ServedInstanceID = %q, want the stored id to win", got)
	}
}

func TestServedInstanceIDFallsBackWhenNoStateExists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "absent.json")
	if got := ServedInstanceID(p, "explicit"); got != "explicit" {
		t.Fatalf("ServedInstanceID = %q, want the caller's own id", got)
	}
}

func TestAClientWithoutAnInstanceIDDialsNoSocket(t *testing.T) {
	t.Setenv(control.CommandSocketVar, "")
	layout := isolatedRuntimeDir(t)
	nameless := layout.Socket("herrscher-command-")
	var config net.ListenConfig
	listener, err := config.Listen(t.Context(), "unix", nameless)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logCleanup(listener.Close()) })
	dialed := make(chan struct{}, 1)
	go func() {
		if conn, acceptErr := listener.Accept(); acceptErr == nil {
			dialed <- struct{}{}
			logCleanup(conn.Close())
		}
	}()
	_, handled, err := ForwardToDaemon(t.Context(), NewMessages(), layout, filepath.Join(t.TempDir(), "absent.json"), "", []string{"session", "list"})
	if handled || err != nil {
		t.Fatalf("ForwardToDaemon = handled %v, %v, want no daemon reached", handled, err)
	}
	if _, err := DaemonDispatch(t.Context(), NewMessages(), layout, "", []string{"session", "list"}); !IsDaemonNotRunning(err) {
		t.Fatalf("DaemonDispatch = %v, want no daemon running", err)
	}
	select {
	case <-dialed:
		t.Fatal("a client without an instance id dialed the socket of an empty id")
	default:
	}
}
