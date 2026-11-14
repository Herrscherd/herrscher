package supervisor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestStoppingHoldsWhileAStuckLoopIsHeldAndClearsWhenItReturns(t *testing.T) {
	grace := 50 * time.Millisecond
	release := make(chan struct{})
	started := make(chan struct{})
	s := NewSupervisor(context.Background(), "/bin/herrscher", testLayout(t))
	s.run = func(ctx context.Context, _ state.Session) {
		close(started)
		<-ctx.Done()
		<-release
	}

	s.Start(state.Session{Name: "demo"})
	<-started
	if s.Stopping("demo") != nil {
		t.Fatal("Stopping reports a stop before any stop was asked")
	}

	if err := s.StopWithin("demo", grace); !errors.Is(err, ErrStillStopping) {
		t.Fatalf("StopWithin = %v, want ErrStillStopping", err)
	}
	settled := s.Stopping("demo")
	if settled == nil {
		t.Fatal("Stopping is nil while the stuck loop is still held")
	}
	close(release)
	select {
	case <-settled:
	case <-time.After(5 * time.Second):
		t.Fatal("the settled channel did not close once the loop returned")
	}
	if s.Stopping("demo") != nil {
		t.Fatal("Stopping still reports a stop after the loop returned")
	}
}
