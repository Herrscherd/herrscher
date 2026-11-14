package supervisor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestStartWithinReturnsStillStoppingAfterTheGrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		grace := time.Second
		release := make(chan struct{})
		defer close(release)
		started := make(chan struct{})
		var runs atomic.Int32
		s := NewSupervisor(context.Background(), "/bin/herrscher", testLayout(t))
		s.run = func(ctx context.Context, _ state.Session) {
			if runs.Add(1) > 1 {
				return
			}
			close(started)
			<-ctx.Done()
			<-release
		}

		s.Start(state.Session{Name: "demo"})
		<-started
		stopped := make(chan error, 1)
		go func() { stopped <- s.Stop("demo") }()
		synctest.Wait()

		result := make(chan error, 1)
		go func() { result <- s.StartWithin(state.Session{Name: "demo"}, grace) }()
		select {
		case err := <-result:
			if !errors.Is(err, ErrStillStopping) {
				t.Fatalf("StartWithin = %v, want ErrStillStopping", err)
			}
		case <-time.After(2 * grace):
			t.Fatal("StartWithin kept waiting for a stuck loop past the grace")
		}
		if got := runs.Load(); got != 1 {
			t.Fatalf("a refused start launched a replacement run: %d runs", got)
		}
	})
}
