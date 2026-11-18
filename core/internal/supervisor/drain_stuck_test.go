package supervisor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestDrainBridgesReturnsStillStoppingWithinTheBoundWhenABridgeNeverExits(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	sup := NewSupervisor(ctx, "herrscher", testLayout(t))
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	started := make(chan struct{})
	sup.run = func(runCtx context.Context, _ state.Session) {
		close(started)
		<-runCtx.Done()
		<-release
	}
	sup.Start(state.Session{Name: "stuck"})
	<-started
	cancel()

	type drainResult struct {
		err     error
		elapsed time.Duration
	}
	results := make(chan drainResult, 1)
	go func() {
		started := time.Now()
		err := sup.DrainBridges()
		results <- drainResult{err: err, elapsed: time.Since(started)}
	}()

	select {
	case got := <-results:
		if !errors.Is(got.err, ErrStillStopping) {
			t.Fatalf("DrainBridges with a bridge that never exits = %v, want ErrStillStopping", got.err)
		}
		lower := DrainBound * 9 / 10
		upper := DrainBound + time.Second
		if got.elapsed < lower || got.elapsed > upper {
			t.Fatalf("DrainBridges returned after %v, want between %v and %v", got.elapsed, lower, upper)
		}
	case <-time.After(DrainBound + 10*time.Second):
		t.Fatal("DrainBridges never returned: the drain bound is not enforced")
	}
}
