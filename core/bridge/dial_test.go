package bridge

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"
)

func TestDialWaitsForASocketThatIsNotThereYet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "late.sock")
		listening := make(chan net.Listener, 1)
		time.AfterFunc(350*time.Millisecond, func() {
			ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", path)
			if err != nil {
				t.Errorf("listen: %v", err)
				return
			}
			listening <- ln
		})
		conn, err := dialHub(context.Background(), path)
		if err != nil {
			t.Fatalf("dial must wait for the socket: %v", err)
		}
		defer closeAndWarn(conn)
		select {
		case ln := <-listening:
			defer closeAndWarn(ln)
		default:
			t.Fatal("dial connected before any listener was up on the path")
		}
	})
}

func TestDialGivesUpWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dialHub(ctx, filepath.Join(t.TempDir(), "never.sock")); err == nil {
		t.Fatal("a cancelled dial must not block")
	}
}
