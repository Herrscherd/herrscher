package host

import (
	"context"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func receiveTurnEvent(t *testing.T, ch <-chan contracts.Event) contracts.Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for turn event")
		return contracts.Event{}
	}
}

func identifiedTestDriver(to chan<- contracts.Event, from <-chan contracts.Event) (*sessionDriver, <-chan contracts.Event) {
	d := newSessionDriver("same-name", nil, to, from)
	d.incarnation = "incarnation-a"
	d.agent = "reviewer"
	emitted := make(chan contracts.Event, 32)
	d.emitTap = func(e contracts.Event) { emitted <- e }
	return d, emitted
}

func assertTurnIdentity(t *testing.T, e contracts.Event, incarnation, turnID, agent string) {
	t.Helper()
	if e.SessionIncarnation != incarnation || e.TurnID != turnID || e.Agent != agent {
		t.Fatalf("%s identity = (%q, %q, %q), want (%q, %q, %q)",
			e.T, e.SessionIncarnation, e.TurnID, e.Agent, incarnation, turnID, agent)
	}
}

func TestDriverPreservesSuppliedTurnIdentityAcrossEveryTurnEvent(t *testing.T) {
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event, 8)
	d, emitted := identifiedTestDriver(toBridge, fromBridge)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.pump(ctx)

	inputSeen := make(chan contracts.Event, 1)
	go func() {
		inputSeen <- <-toBridge
		fromBridge <- contracts.Event{T: "thinking", Text: "hmm"}
		fromBridge <- contracts.Event{
			T: "status", Text: "working",
			SessionIncarnation: "spoofed", TurnID: "spoofed", Agent: "spoofed",
		}
		fromBridge <- contracts.Event{T: "chunk", Text: "partial"}
		fromBridge <- contracts.Event{T: "reset"}
		fromBridge <- contracts.Event{T: "reply", Text: "done", Done: true}
	}()

	const supplied = "client.turn-01"
	reply, ok := d.SeedAndWaitWithTurnID(ctx, "do it", supplied)
	if !ok || reply != "done" {
		t.Fatalf("SeedAndWaitWithTurnID = %q, %v, want done, true", reply, ok)
	}
	input := receiveTurnEvent(t, inputSeen)
	assertTurnIdentity(t, input, "incarnation-a", supplied, "reviewer")

	wantTypes := []string{"human", "thinking", "status", "chunk", "reset", "reply"}
	for _, wantType := range wantTypes {
		event := receiveTurnEvent(t, emitted)
		if event.T != wantType {
			t.Fatalf("event type = %q, want %q", event.T, wantType)
		}
		assertTurnIdentity(t, event, "incarnation-a", supplied, "reviewer")
	}
}

func TestDriverAutoTurnIDIsStableWithinTurnAndDifferentAcrossTurns(t *testing.T) {
	toBridge := make(chan contracts.Event, 2)
	fromBridge := make(chan contracts.Event, 2)
	d, emitted := identifiedTestDriver(toBridge, fromBridge)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.pump(ctx)

	d.queue <- contracts.Event{T: "input", Who: "alice", Text: "first"}
	d.queue <- contracts.Event{T: "input", Who: "alice", Text: "second"}

	firstInput := receiveTurnEvent(t, toBridge)
	if firstInput.TurnID == "" {
		t.Fatal("first auto-generated turn id is empty")
	}
	fromBridge <- contracts.Event{T: "reply", Text: "one", Done: true}
	secondInput := receiveTurnEvent(t, toBridge)
	if secondInput.TurnID == "" {
		t.Fatal("second auto-generated turn id is empty")
	}
	if secondInput.TurnID == firstInput.TurnID {
		t.Fatalf("two turns reused auto-generated id %q", firstInput.TurnID)
	}
	fromBridge <- contracts.Event{T: "reply", Text: "two", Done: true}

	firstHuman := receiveTurnEvent(t, emitted)
	firstReply := receiveTurnEvent(t, emitted)
	secondHuman := receiveTurnEvent(t, emitted)
	secondReply := receiveTurnEvent(t, emitted)
	for _, event := range []contracts.Event{firstHuman, firstReply} {
		assertTurnIdentity(t, event, "incarnation-a", firstInput.TurnID, "reviewer")
	}
	for _, event := range []contracts.Event{secondHuman, secondReply} {
		assertTurnIdentity(t, event, "incarnation-a", secondInput.TurnID, "reviewer")
	}
}

func TestDriverAbandonedEventCarriesTurnIdentity(t *testing.T) {
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event)
	close(fromBridge)
	d, emitted := identifiedTestDriver(toBridge, fromBridge)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.pump(ctx)

	d.queue <- contracts.Event{T: "input", Who: "alice", Text: "will fail", TurnID: "turn-abandoned"}
	input := receiveTurnEvent(t, toBridge)
	assertTurnIdentity(t, input, "incarnation-a", "turn-abandoned", "reviewer")

	human := receiveTurnEvent(t, emitted)
	abandoned := receiveTurnEvent(t, emitted)
	if human.T != "human" || abandoned.T != "abandoned" {
		t.Fatalf("events = [%s, %s], want [human, abandoned]", human.T, abandoned.T)
	}
	assertTurnIdentity(t, abandoned, "incarnation-a", "turn-abandoned", "reviewer")
}

func TestDriverClearsIdentityForPickOutsideTurn(t *testing.T) {
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event, 1)
	d, emitted := identifiedTestDriver(toBridge, fromBridge)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.pump(ctx)

	d.Pick("2")
	pick := receiveTurnEvent(t, toBridge)
	if pick.SessionIncarnation != "" || pick.TurnID != "" || pick.Agent != "" {
		t.Fatalf("pick input has dishonest identity: %+v", pick)
	}
	fromBridge <- contracts.Event{
		T: "reply", Text: "picked", Done: true,
		SessionIncarnation: "spoofed", TurnID: "spoofed", Agent: "spoofed",
	}
	reply := receiveTurnEvent(t, emitted)
	if reply.SessionIncarnation != "" || reply.TurnID != "" || reply.Agent != "" {
		t.Fatalf("pick reply has dishonest identity: %+v", reply)
	}
}

func TestDriversWithSameNameRemainDistinguishableByIncarnation(t *testing.T) {
	run := func(incarnation string) contracts.Event {
		toBridge := make(chan contracts.Event, 1)
		fromBridge := make(chan contracts.Event, 1)
		d, emitted := identifiedTestDriver(toBridge, fromBridge)
		d.incarnation = incarnation
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go d.pump(ctx)
		d.queue <- contracts.Event{T: "input", Text: "task", TurnID: "same-turn"}
		_ = receiveTurnEvent(t, toBridge)
		fromBridge <- contracts.Event{T: "reply", Text: "done", Done: true}
		return receiveTurnEvent(t, emitted)
	}

	oldEvent := run("old-incarnation")
	newEvent := run("new-incarnation")
	if oldEvent.SessionIncarnation == newEvent.SessionIncarnation {
		t.Fatalf("same-name session events are indistinguishable: old=%+v new=%+v", oldEvent, newEvent)
	}
}
