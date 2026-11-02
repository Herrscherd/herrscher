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
}

func (b *budgetedTurn) sawStatus(want string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.tapped {
		if e.T == "status" && strings.Contains(e.Text, want) {
			return true
		}
	}
	return false
}

func (b *budgetedTurn) awaitStatus(want string) bool {
	deadline := time.Now().Add(time.Second)
	for !b.sawStatus(want) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
	return true
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
				if !b.awaitStatus("interrompu") {
					t.Fatal("turn was interrupted without telling the subscriber why")
				}
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

	for i := 0; i < 3; i++ {
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
