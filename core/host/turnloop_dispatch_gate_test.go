package host

import (
	"context"
	"strings"
	"sync"
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
	// a turn against an over-budget session).
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case ev := <-toBridge:
			if ev.T == "input" {
				t.Fatalf("driver opened a turn while paused: wrote %+v", ev)
			}
		case <-deadline:
			break loop
		}
	}
}

// TestPausedReachesEmitTap proves the paused event reaches LIVE subscribers
// via the same fanOut path as every other subscriber-visible frame (status,
// reply, etc.) — not only the backend bridge. Before this fix, emitPaused
// wrote solely to d.toBridge, which no subscriber tap observes; a subscriber
// only learned of the pause on the app's next status poll, not live.
func TestPausedReachesEmitTap(t *testing.T) {
	a := &fanRecorder{}
	a.feed("hello")

	toBridge := make(chan contracts.Event, 4)
	fromBridge := make(chan contracts.Event, 4)
	d := newSessionDriver("s1",
		[]contracts.GatewaySet{{Gateway: a, Reader: a}},
		toBridge, fromBridge)
	d.gate = fixedGate{reason: "cost"}
	var tapped []contracts.Event
	var mu sync.Mutex
	d.emitTap = func(e contracts.Event) {
		mu.Lock()
		tapped = append(tapped, e)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.run(ctx)

	deadline := time.After(time.Second)
	for {
		mu.Lock()
		for _, e := range tapped {
			if e.T == "paused" && e.Text == "cost" {
				mu.Unlock()
				return
			}
		}
		mu.Unlock()
		select {
		case <-deadline:
			t.Fatal("emitTap never saw a paused event with reason \"cost\"")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestRefusedInputAcknowledgedNotSilentlyDropped proves that when an input
// event arrives while the gate is tripped, the dequeued input is not just
// discarded: the subscriber tap sees BOTH the paused event and an explicit
// refusal status frame naming the refused input, and no turn is opened for
// it (no "human"/"input" frame reaches the tap or the bridge for that
// message).
func TestRefusedInputAcknowledgedNotSilentlyDropped(t *testing.T) {
	a := &fanRecorder{}
	a.feed("spend more")

	toBridge := make(chan contracts.Event, 4)
	fromBridge := make(chan contracts.Event, 4)
	d := newSessionDriver("s1",
		[]contracts.GatewaySet{{Gateway: a, Reader: a}},
		toBridge, fromBridge)
	d.gate = fixedGate{reason: "cost"}
	var tapped []contracts.Event
	var mu sync.Mutex
	d.emitTap = func(e contracts.Event) {
		mu.Lock()
		tapped = append(tapped, e)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.run(ctx)

	deadline := time.After(time.Second)
	sawPaused, sawRefusalStatus := false, false
	for {
		mu.Lock()
		for _, e := range tapped {
			if e.T == "human" {
				mu.Unlock()
				t.Fatalf("a turn was opened for input refused by the budget gate: %+v", e)
			}
			if e.T == "paused" {
				sawPaused = true
			}
			if e.T == "status" && strings.Contains(e.Text, "refus") {
				sawRefusalStatus = true
			}
		}
		gotBoth := sawPaused && sawRefusalStatus
		mu.Unlock()
		if gotBoth {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("did not see both paused and refusal status on emitTap: paused=%v refusalStatus=%v", sawPaused, sawRefusalStatus)
		case <-time.After(5 * time.Millisecond):
		}
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
