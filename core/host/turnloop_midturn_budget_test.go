package host

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type budgetedTurn struct {
	toBridge, fromBridge chan contracts.Event
	mu                   sync.Mutex
	tapped               []contracts.Event
	changed              changeNotifier
}

func (b *budgetedTurn) sawStatus(want string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for idx := range b.tapped {
		e := b.tapped[idx]
		if e.T == "status" && strings.Contains(e.Text, want) {
			return true
		}
	}
	return false
}

func (b *budgetedTurn) awaitStatus(t *testing.T, want string) {
	t.Helper()
	waitFor(t, &b.changed, func() bool { return b.sawStatus(want) }, "a status telling why the turn was interrupted")
}

func startBudgetedTurn(t *testing.T, headroom uint64) *budgetedTurn {
	t.Helper()
	a := &fanRecorder{}
	a.feed("compute something expensive")

	toBridge := make(chan contracts.Event, 8)
	fromBridge := make(chan contracts.Event, 8)
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: a, Reader: a}}, toBridge, fromBridge)
	d.gate = fixedGate{headroom: headroom, capped: true}
	b := &budgetedTurn{toBridge: toBridge, fromBridge: fromBridge}
	d.emitTap = func(e contracts.Event) {
		b.mu.Lock()
		b.tapped = append(b.tapped, e)
		b.mu.Unlock()
		b.changed.signal()
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.run(ctx)

	select {
	case in := <-toBridge:
		if in.T != "input" {
			t.Fatalf("first frame to the bridge = %+v, want input", in)
		}
	case <-time.After(time.Second):
		t.Fatal("driver never opened a turn")
	}
	return b
}

func TestRunawayTurnInterruptedOnTokenCap(t *testing.T) {
	b := startBudgetedTurn(t, 100)

	b.fromBridge <- contracts.Event{T: "chunk", Text: "thinking hard", Tokens: 80, TokensIn: 40}

	deadline := time.After(time.Second)
	for {
		select {
		case ev := <-b.toBridge:
			if ev.T == "interrupt" {
				b.awaitStatus(t, "interrompu")
				return
			}
		case <-deadline:
			t.Fatal("runaway turn was never interrupted")
		}
	}
}

func TestBudgetedTurnUnderHeadroomRunsOn(t *testing.T) {
	b := startBudgetedTurn(t, 100)

	b.fromBridge <- contracts.Event{T: "chunk", Text: "cheap", Tokens: 10, TokensIn: 5}
	b.fromBridge <- contracts.Event{T: "status", Text: "no usage reported"}
	b.fromBridge <- contracts.Event{T: "reply", Text: "done", Done: true, Tokens: 10, TokensIn: 5}

	select {
	case ev := <-b.toBridge:
		t.Fatalf("turn under headroom must not be interrupted, got %+v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestRunawayTurnInterruptedOnce(t *testing.T) {
	b := startBudgetedTurn(t, 100)

	for range 3 {
		b.fromBridge <- contracts.Event{T: "chunk", Text: "more", Tokens: 200, TokensIn: 100}
	}

	interrupts := 0
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case ev := <-b.toBridge:
			if ev.T == "interrupt" {
				interrupts++
			}
		case <-deadline:
			if interrupts != 1 {
				t.Fatalf("interrupt frames = %d, want exactly 1", interrupts)
			}
			return
		}
	}
}
