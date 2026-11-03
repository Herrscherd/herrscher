package bridge

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

func TestDialWaitsForASocketThatIsNotThereYet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "late.sock")
	go func() {
		time.Sleep(150 * time.Millisecond)
		acc, err := control.Accept(t.Context(), path)
		if err != nil {
			return
		}
		t.Cleanup(func() { closeAndWarn(acc) })
	}()

	conn, err := dialHub(context.Background(), path)
	if err != nil {
		t.Fatalf("dial must wait for the socket: %v", err)
	}
	closeAndWarn(conn)
}

func TestDialGivesUpWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dialHub(ctx, filepath.Join(t.TempDir(), "never.sock")); err == nil {
		t.Fatal("a cancelled dial must not block")
	}
}
