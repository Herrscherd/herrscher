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

func TestRestartWithinReturnsStillStoppingAfterTheGrace(t *testing.T) {
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

		result := make(chan error, 1)
		go func() { result <- s.RestartWithin(state.Session{Name: "demo"}, grace) }()
		select {
		case err := <-result:
			if !errors.Is(err, ErrStillStopping) {
				t.Fatalf("RestartWithin = %v, want ErrStillStopping", err)
			}
		case <-time.After(2 * grace):
			t.Fatal("RestartWithin kept waiting for a stuck stop past the grace")
		}
		if got := runs.Load(); got != 1 {
			t.Fatalf("a refused restart launched a replacement run: %d runs", got)
		}
	})
}
