package host

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/manager"
)

type fixedGate struct {
	reason   string
	headroom uint64
	capped   bool
}

func (g fixedGate) Check(string) manager.BudgetCheck {
	return manager.BudgetCheck{Reason: g.reason, Headroom: g.headroom, Capped: g.capped}
}

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
