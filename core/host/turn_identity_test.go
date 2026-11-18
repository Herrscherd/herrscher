package host

import (
	"context"
	"io"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
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

func relayTurnFrames(cancel context.CancelFunc, toBridge <-chan contracts.Event, frames, fromBridge chan<- contracts.Event, count int) {
	for range count {
		select {
		case event := <-toBridge:
			frames <- event
		case <-time.After(time.Second):
			cancel()
			return
		}
	}
	fromBridge <- contracts.Event{T: eventwire.EventReply, Text: "done", Done: true}
}

func identifiedTestDriver(to chan<- contracts.Event, from <-chan contracts.Event) (*sessionDriver, <-chan contracts.Event) {
	d := newSessionDriver(newHostMessages(), "same-name", nil, to, from, io.Discard)
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
		fromBridge <- contracts.Event{T: eventwire.EventThinking, Text: "hmm"}
		fromBridge <- contracts.Event{
			T: eventwire.EventStatus, Text: "working",
			SessionIncarnation: "spoofed", TurnID: "spoofed", Agent: "spoofed",
		}
		fromBridge <- contracts.Event{T: eventwire.EventChunk, Text: "partial"}
		fromBridge <- contracts.Event{T: eventwire.EventReset}
		fromBridge <- contracts.Event{T: eventwire.EventReply, Text: "done", Done: true}
	}()

	const supplied = "client.turn-01"
	reply, ok := d.SeedAndWaitWithTurnID(ctx, "do it", supplied)
	if !ok || reply != "done" {
		t.Fatalf("SeedAndWaitWithTurnID = %q, %v, want done, true", reply, ok)
	}
	input := receiveTurnEvent(t, inputSeen)
	assertTurnIdentity(t, input, "incarnation-a", supplied, "reviewer")

	wantTypes := []string{eventwire.EventHuman, eventwire.EventThinking, eventwire.EventStatus, eventwire.EventChunk, eventwire.EventReset, eventwire.EventReply}
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

	d.queue <- queued{ev: contracts.Event{T: eventwire.EventInput, Who: "alice", Text: "first"}}
	d.queue <- queued{ev: contracts.Event{T: eventwire.EventInput, Who: "alice", Text: "second"}}

	firstInput := receiveTurnEvent(t, toBridge)
	if firstInput.TurnID == "" {
		t.Fatal("first auto-generated turn id is empty")
	}
	fromBridge <- contracts.Event{T: eventwire.EventReply, Text: "one", Done: true}
	secondInput := receiveTurnEvent(t, toBridge)
	if secondInput.TurnID == "" {
		t.Fatal("second auto-generated turn id is empty")
	}
	if secondInput.TurnID == firstInput.TurnID {
		t.Fatalf("two turns reused auto-generated id %q", firstInput.TurnID)
	}
	fromBridge <- contracts.Event{T: eventwire.EventReply, Text: "two", Done: true}

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

	d.queue <- queued{ev: contracts.Event{T: eventwire.EventInput, Who: "alice", Text: "will fail", TurnID: "turn-abandoned"}}
	input := receiveTurnEvent(t, toBridge)
	assertTurnIdentity(t, input, "incarnation-a", "turn-abandoned", "reviewer")

	human := receiveTurnEvent(t, emitted)
	abandoned := receiveTurnEvent(t, emitted)
	if human.T != eventwire.EventHuman || abandoned.T != eventwire.EventAbandoned {
		t.Fatalf("events = [%s, %s], want [human, abandoned]", human.T, abandoned.T)
	}
	assertTurnIdentity(t, abandoned, "incarnation-a", "turn-abandoned", "reviewer")
}

func TestPickReplyOutsideTurnCarriesItsOwnTurnID(t *testing.T) {
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event, 8)
	d, emitted := identifiedTestDriver(toBridge, fromBridge)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.pump(ctx)

	d.queue <- queued{ev: contracts.Event{T: eventwire.EventInput, Text: "task", TurnID: "turn-last"}}
	_ = receiveTurnEvent(t, toBridge)
	fromBridge <- contracts.Event{T: eventwire.EventReply, Text: "done", Done: true}
	_ = receiveTurnEvent(t, emitted)

	d.Pick("2")
	pick := receiveTurnEvent(t, toBridge)
	if pick.TurnID == "" || pick.TurnID == "turn-last" {
		t.Fatalf("pick TurnID = %q, want a fresh non-empty id distinct from the last turn", pick.TurnID)
	}
	if pick.SessionIncarnation != "" || pick.Agent != "" {
		t.Fatalf("pick input has dishonest identity: %+v", pick)
	}
	fromBridge <- contracts.Event{T: eventwire.EventReply, Text: "picked", Done: true, TurnID: "turn-last"}
	reply := receiveTurnEvent(t, emitted)
	if reply.TurnID != pick.TurnID {
		t.Fatalf("pick reply TurnID = %q, want the driver's %q, not the forged one", reply.TurnID, pick.TurnID)
	}
	if reply.SessionIncarnation != "" || reply.Agent != "" {
		t.Fatalf("pick reply has dishonest identity: %+v", reply)
	}
}

func TestOffTurnEventDropsAForgedTurnID(t *testing.T) {
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event, 1)
	d, emitted := identifiedTestDriver(toBridge, fromBridge)

	d.fanOut(context.Background(), contracts.Event{T: eventwire.EventStatus, Text: "hors tour", TurnID: "turn-last"})
	if got := receiveTurnEvent(t, emitted); got.TurnID != "" {
		t.Fatalf("off-turn event TurnID = %q, want none: a forged identifier must not leave the driver", got.TurnID)
	}
}

func TestPickReplyIsRecordedWithItsTurnID(t *testing.T) {
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event, 8)
	d, _ := identifiedTestDriver(toBridge, fromBridge)
	recorded := make(chan state.TranscriptEntry, 8)
	d.sink.Transcript = func(e state.TranscriptEntry) { recorded <- e }
	d.terms = func() modelTerms { return modelTerms{rates: sonnetRates()} }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.pump(ctx)

	d.Pick("2")
	pick := receiveTurnEvent(t, toBridge)
	fromBridge <- contracts.Event{T: eventwire.EventReply, Text: "picked", Done: true, TurnID: "turn-last"}

	var entry state.TranscriptEntry
	select {
	case entry = <-recorded:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the pick reply to be recorded")
	}
	if entry.Role != "assistant" || entry.Text != "picked" || entry.TurnID != pick.TurnID {
		t.Fatalf("pick reply transcript = %+v, want assistant picked with the driver's TurnID %q", entry, pick.TurnID)
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
		d.queue <- queued{ev: contracts.Event{T: eventwire.EventInput, Text: "task", TurnID: "same-turn"}}
		_ = receiveTurnEvent(t, toBridge)
		fromBridge <- contracts.Event{T: eventwire.EventReply, Text: "done", Done: true}
		return receiveTurnEvent(t, emitted)
	}

	oldEvent := run("old-incarnation")
	newEvent := run("new-incarnation")
	if oldEvent.SessionIncarnation == newEvent.SessionIncarnation {
		t.Fatalf("same-name session events are indistinguishable: old=%+v new=%+v", oldEvent, newEvent)
	}
}

func TestDriverSendsAnEmptyLanguageFrameWhenTheSessionHasNoLanguage(t *testing.T) {
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event, 8)
	d, _ := identifiedTestDriver(toBridge, fromBridge)
	d.language = func() i18n.Language { return 0 }
	d.terms = func() modelTerms { return modelTerms{} }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.pump(ctx)

	frames := make(chan contracts.Event, 2)
	go relayTurnFrames(cancel, toBridge, frames, fromBridge, 2)
	if _, ok := d.SeedAndWaitWithTurnID(ctx, "bonjour", "turn-1"); !ok {
		t.Fatal("the seeded turn must complete")
	}

	if first := receiveTurnEvent(t, frames); first.T != eventwire.EventLanguage || first.Value != "" {
		t.Fatalf("first frame = %q %q, want the language frame with an empty value", first.T, first.Value)
	}
}

func TestDriverTellsTheBridgeTheSessionLanguageBeforeEachTurn(t *testing.T) {
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event, 8)
	d, _ := identifiedTestDriver(toBridge, fromBridge)
	d.language = func() i18n.Language { return i18n.French }
	d.terms = func() modelTerms { return modelTerms{} }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.pump(ctx)

	frames := make(chan contracts.Event, 2)
	go relayTurnFrames(cancel, toBridge, frames, fromBridge, 2)
	if _, ok := d.SeedAndWaitWithTurnID(ctx, "bonjour", "turn-1"); !ok {
		t.Fatal("the seeded turn must complete")
	}

	if first := receiveTurnEvent(t, frames); first.T != eventwire.EventLanguage || first.Value != i18n.French.String() {
		t.Fatalf("first frame = %q %q, want the language frame fr", first.T, first.Value)
	}
	if second := receiveTurnEvent(t, frames); second.T != eventwire.EventInput || second.Text != "bonjour" {
		t.Fatalf("second frame = %q %q, want the input", second.T, second.Text)
	}
	if got := d.sessionInfo().Language; got != i18n.French {
		t.Fatalf("sessionInfo Language = %v, want %v", got, i18n.French)
	}
}
