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
	waiting := make(chan struct{}, 1)
	retry := make(chan time.Time)
	after := func(time.Duration) <-chan time.Time {
		waiting <- struct{}{}
		return retry
	}
	type dialed struct {
		conn *control.Conn
		err  error
	}
	result := make(chan dialed, 1)
	go func() {
		conn, err := dialHubAfter(context.Background(), path, after)
		result <- dialed{conn, err}
	}()

	<-waiting
	acc, err := control.Accept(t.Context(), path)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer closeAndWarn(acc)
	retry <- time.Now()

	got := <-result
	if got.err != nil {
		t.Fatalf("dial must wait for the socket: %v", got.err)
	}
	closeAndWarn(got.conn)
}

func TestDialGivesUpWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dialHub(ctx, filepath.Join(t.TempDir(), "never.sock")); err == nil {
		t.Fatal("a cancelled dial must not block")
	}
}
