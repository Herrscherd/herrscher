package supervisor

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestStopWithinReturnsStillStoppingAfterTheGrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		grace := time.Second
		release := make(chan struct{})
		defer close(release)
		started := make(chan struct{})
		s := NewSupervisor(context.Background(), "/bin/herrscher", testLayout(t))
		s.run = func(ctx context.Context, _ state.Session) {
			close(started)
			<-ctx.Done()
			<-release
		}

		s.Start(state.Session{Name: "demo"})
		<-started

		result := make(chan error, 1)
		go func() { result <- s.StopWithin("demo", grace) }()
		select {
		case err := <-result:
			if !errors.Is(err, ErrStillStopping) {
				t.Fatalf("StopWithin = %v, want ErrStillStopping", err)
			}
		case <-time.After(2 * grace):
			t.Fatal("StopWithin kept waiting for a stuck loop past the grace")
		}
	})
}
