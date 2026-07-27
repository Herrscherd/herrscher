package host

import (
	"context"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// fixedGate is a test-local fake budgetGate whose CheckAfterTurn always
// returns a fixed reason, simulating a session that is already over budget
// before the next turn is dispatched.
type fixedGate struct{ reason string }

func (g fixedGate) CheckAfterTurn(string) string { return g.reason }

// TestDispatchRefusedWhilePaused proves the dispatch point — not just the
// post-turn check — refuses to open a new turn when the gate reports the
// session is over budget. No input frame should reach the bridge, so
// runTurn/awaitTurn never engage the backend; instead a paused event is
// emitted.
func TestDispatchRefusedWhilePaused(t *testing.T) {
	a := &fanRecorder{}
	a.feed("hello")

	toBridge := make(chan contracts.Event, 4)
	fromBridge := make(chan contracts.Event, 4)
	d := newSessionDriver("s1",
		[]contracts.GatewaySet{{Gateway: a, Reader: a}},
		toBridge, fromBridge)
	d.gate = fixedGate{reason: "cost"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.run(ctx)

	// The driver must never write an input frame (that would mean it opened
	// a turn against an over-budget session); it must instead emit a paused
	// event.
	sawPaused := false
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case ev := <-toBridge:
			if ev.T == "input" {
				t.Fatalf("driver opened a turn while paused: wrote %+v", ev)
			}
			if ev.T == "paused" {
				sawPaused = true
				break loop
			}
		case <-deadline:
			break loop
		}
	}
	if !sawPaused {
		t.Fatal("driver did not emit a paused event while over budget")
	}
}

// TestDispatchProceedsWhenNotPaused is the regression guard: with a gate that
// never trips (matching noBudgetGate's contract), the same event still opens
// a turn normally.
func TestDispatchProceedsWhenNotPaused(t *testing.T) {
	a := &fanRecorder{}
	a.feed("hello")

	toBridge := make(chan contracts.Event, 4)
	fromBridge := make(chan contracts.Event, 4)
	d := newSessionDriver("s1",
		[]contracts.GatewaySet{{Gateway: a, Reader: a}},
		toBridge, fromBridge)
	d.gate = fixedGate{reason: ""}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.run(ctx)

	select {
	case in := <-toBridge:
		if in.T != "input" || in.Text != "hello" {
			t.Fatalf("driver wrote %+v, want input/hello", in)
		}
	case <-time.After(time.Second):
		t.Fatal("driver did not open a turn when not paused")
	}
}
