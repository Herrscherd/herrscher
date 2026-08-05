package bridge

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

// The daemon starts a bridge and opens its control socket from two different
// places, so a fresh bridge routinely reaches the socket before it exists.
// Without a wait it dies on the spot and comes back only through the restart
// backoff — a crash in the log, and a session that answers nothing while it
// happens.
func TestDialWaitsForASocketThatIsNotThereYet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "late.sock")
	go func() {
		time.Sleep(150 * time.Millisecond)
		acc, err := control.Accept(path)
		if err != nil {
			return
		}
		t.Cleanup(func() { _ = acc.Close() })
	}()

	conn, err := dialHub(context.Background(), path)
	if err != nil {
		t.Fatalf("dial must wait for the socket: %v", err)
	}
	_ = conn.Close()
}

// A bridge whose daemon is truly gone must still exit, so the supervisor gets to
// decide rather than the process hanging on a socket that will never appear.
func TestDialGivesUpWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dialHub(ctx, filepath.Join(t.TempDir(), "never.sock")); err == nil {
		t.Fatal("a cancelled dial must not block")
	}
}
