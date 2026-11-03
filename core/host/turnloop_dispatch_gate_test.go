package host

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type fixedGate struct {
	reason   string
	headroom uint64
	capped   bool
}

func (g fixedGate) Check(string) (string, uint64, bool) { return g.reason, g.headroom, g.capped }

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
	tap := &refusalTap{}
	d.emitTap = tap.record

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.run(ctx)

	awaitRefusalAcknowledged(t, tap)
}

type refusalTap struct {
	mu     sync.Mutex
	events []contracts.Event
}

func (k *refusalTap) record(e contracts.Event) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.events = append(k.events, e)
}

type refusalScan struct {
	paused  bool
	refused bool
}

func (k *refusalTap) scanRefusal(t *testing.T) refusalScan {
	t.Helper()
	k.mu.Lock()
	defer k.mu.Unlock()
	var scan refusalScan
	for i := range k.events {
		e := k.events[i]
		if e.T == "human" {
			t.Fatalf("a turn was opened for input refused by the budget gate: %+v", e)
		}
		scan.paused = scan.paused || e.T == "paused"
		scan.refused = scan.refused || (e.T == "status" && strings.Contains(e.Text, "refus"))
	}
	return scan
}

func awaitRefusalAcknowledged(t *testing.T, tap *refusalTap) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		scan := tap.scanRefusal(t)
		if scan.paused && scan.refused {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("did not see both paused and refusal status on emitTap: paused=%v refusalStatus=%v", scan.paused, scan.refused)
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
