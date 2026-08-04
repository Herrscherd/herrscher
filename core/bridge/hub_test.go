package bridge

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// recordSink captures the events runHub emits.
type recordSink struct{ events []contracts.Event }

func (s *recordSink) Emit(e contracts.Event) { s.events = append(s.events, e) }

// fakeBackend emits one text event (and optionally a terminal result carrying
// cost) then returns a fixed reply.
type fakeBackend struct {
	reply string
	cost  float64
}

func (b fakeBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	if onEvent != nil {
		onEvent(contracts.BackendEvent{Kind: "text", Detail: "thinking"})
		if b.cost > 0 {
			onEvent(contracts.BackendEvent{Kind: "result", Cost: b.cost})
		}
	}
	return b.reply, nil
}
func (fakeBackend) Close() error { return nil }

// TestRunHubReplyCarriesCost proves the terminal result's cost is captured and
// attached to the reply{done} (the result event itself carries no transcript and
// is dropped), so the hub can render cost in the progress summary.
func TestRunHubReplyCarriesCost(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "go"}
	close(in)

	runHubTurns(context.Background(), in, sink, fakeBackend{reply: "ok", cost: 0.0042}, nil)

	last := sink.events[len(sink.events)-1]
	if last.T != "reply" || !last.Done || last.Cost != 0.0042 {
		t.Fatalf("reply must carry cost 0.0042; got %+v", last)
	}
	for _, e := range sink.events {
		if e.T == "result" {
			t.Fatalf("result event must not be forwarded over the bus; got %+v", e)
		}
	}
}

func TestEmitBackendEventThinking(t *testing.T) {
	sink := &recordSink{}
	emitBackendEvent(sink, contracts.BackendEvent{Kind: "thinking", Detail: "je réfléchis"}, 0, 0, 0, 0)
	want := []contracts.Event{{T: "thinking", Text: "je réfléchis"}}
	if !reflect.DeepEqual(sink.events, want) {
		t.Fatalf("emitted %+v, want %+v", sink.events, want)
	}
}

// tokenBackend emits a live usage event, a text chunk, then a terminal result
// carrying the final token count.
type tokenBackend struct{ reply string }

func (b tokenBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	onEvent(contracts.BackendEvent{Kind: "usage", InTokens: 10, OutTokens: 20})
	onEvent(contracts.BackendEvent{Kind: "text", Detail: "working"})
	onEvent(contracts.BackendEvent{Kind: "result", InTokens: 30, OutTokens: 55, Cost: 0.001})
	return b.reply, nil
}
func (tokenBackend) Close() error { return nil }

// TestRunOneTurn_StampsTokens proves the live token count rides on chunk events
// and the final count lands on the terminal reply.
func TestRunOneTurn_StampsTokens(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "go"}
	close(in)

	runHubTurns(context.Background(), in, sink, tokenBackend{reply: "done"}, nil)

	var chunkTokens int
	for _, e := range sink.events {
		if e.T == "chunk" {
			chunkTokens = e.Tokens
		}
	}
	if chunkTokens != 20 {
		t.Fatalf("chunk should carry live tokens 20; got %d", chunkTokens)
	}
	last := sink.events[len(sink.events)-1]
	if last.T != "reply" || last.Tokens != 55 {
		t.Fatalf("reply must carry final tokens 55; got %+v", last)
	}
}

// agenticBackend is a turn made of several assistant messages, the way a turn
// with tool calls actually streams: each message reports its OWN usage, never
// the turn so far.
type agenticBackend struct{ reply string }

func (b agenticBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	for i := 0; i < 3; i++ {
		onEvent(contracts.BackendEvent{Kind: "usage", InTokens: 100, OutTokens: 20})
		onEvent(contracts.BackendEvent{Kind: "text", Detail: "step"})
	}
	onEvent(contracts.BackendEvent{Kind: "result", InTokens: 300, OutTokens: 60, Cost: 0.002})
	return b.reply, nil
}
func (agenticBackend) Close() error { return nil }

// TestRunOneTurn_AccumulatesUsageAcrossMessages proves the live output count
// grows over a multi-message turn instead of tracking the last message's size.
// Without the sum, a turn that streams thirty 20-token messages would forever
// report 20, and the host's mid-turn budget guard — which compares that number
// to the turn's headroom — would never see the runaway it exists to cut.
func TestRunOneTurn_AccumulatesUsageAcrossMessages(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "go"}
	close(in)

	runHubTurns(context.Background(), in, sink, agenticBackend{reply: "done"}, nil)

	var chunks []int
	for _, e := range sink.events {
		if e.T == "chunk" {
			chunks = append(chunks, e.Tokens)
		}
	}
	if want := []int{20, 40, 60}; !reflect.DeepEqual(chunks, want) {
		t.Fatalf("live output tokens = %v, want %v (a running sum)", chunks, want)
	}
	last := sink.events[len(sink.events)-1]
	if last.T != "reply" || last.Tokens != 60 || last.TokensIn != 300 {
		t.Fatalf("reply must carry the result's authoritative totals, not the sum; got %+v", last)
	}
}

// cacheBackend emits a terminal result carrying the full token breakdown
// (input/output + prompt-cache read/creation).
type cacheBackend struct{ reply string }

func (b cacheBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	onEvent(contracts.BackendEvent{Kind: "usage", InTokens: 10, OutTokens: 20, CacheRead: 12, CacheCreate: 3})
	onEvent(contracts.BackendEvent{Kind: "text", Detail: "working"})
	onEvent(contracts.BackendEvent{Kind: "result", InTokens: 30, OutTokens: 55, CacheRead: 12, CacheCreate: 3, Cost: 0.001})
	return b.reply, nil
}
func (cacheBackend) Close() error { return nil }

// TestRunOneTurn_StampsTokenBreakdown proves the reply carries the full token
// breakdown (input/cache), not just output.
func TestRunOneTurn_StampsTokenBreakdown(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "go"}
	close(in)

	runHubTurns(context.Background(), in, sink, cacheBackend{reply: "done"}, nil)

	last := sink.events[len(sink.events)-1]
	if last.T != "reply" || last.TokensIn != 30 || last.CacheRead != 12 || last.CacheCreate != 3 {
		t.Fatalf("reply must carry token breakdown; got %+v", last)
	}
}

// blockingBackend blocks in Respond until its turn ctx is cancelled, then
// returns the ctx error — standing in for a claude turn that only stops on
// interrupt.
type blockingBackend struct{ started chan struct{} }

func (b blockingBackend) Respond(ctx context.Context, _ contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	close(b.started)
	<-ctx.Done()
	return "", ctx.Err()
}
func (blockingBackend) Close() error { return nil }

// TestInbound_InterruptCancelsTurn proves an interrupt (via the controller)
// cancels an in-flight turn so it terminates with a reply instead of hanging.
func TestInbound_InterruptCancelsTurn(t *testing.T) {
	ctrl := &turnController{}
	sink := &recordSink{}
	in := make(chan contracts.Event)
	be := blockingBackend{started: make(chan struct{})}

	done := make(chan struct{})
	go func() {
		runHubTurnsCtl(context.Background(), in, sink, be, nil, ctrl, nil, nil)
		close(done)
	}()

	in <- contracts.Event{T: "input", Text: "go"}
	<-be.started // turn is now blocked in Respond
	ctrl.interrupt()

	close(in)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("interrupt did not cancel the in-flight turn")
	}
	last := sink.events[len(sink.events)-1]
	if last.T != "reply" || !last.Done {
		t.Fatalf("interrupted turn must still emit reply{done}; got %+v", last)
	}
}

func TestRunHubOneTurn(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 2)
	in <- contracts.Event{T: "input", Who: "alice", Text: "refactor"}
	close(in)

	be := fakeBackend{reply: "done · 4 files"}
	runHubTurns(context.Background(), in, sink, be, nil)

	want := []contracts.Event{
		{T: "chunk", Text: "thinking"},
		{T: "reply", Text: "done · 4 files", Done: true},
	}
	if len(sink.events) != len(want) {
		t.Fatalf("emitted %+v, want %+v", sink.events, want)
	}
	for i := range want {
		if !reflect.DeepEqual(sink.events[i], want[i]) {
			t.Errorf("event %d = %+v, want %+v", i, sink.events[i], want[i])
		}
	}
}

func TestRunHubTurnEventsPreserveInputIdentity(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{
		T: "input", Text: "go",
		SessionIncarnation: "incarnation-a",
		TurnID:             "turn-a",
		Agent:              "reviewer",
	}
	close(in)

	runHubTurns(context.Background(), in, sink, fakeBackend{reply: "done"}, nil)

	if len(sink.events) != 2 {
		t.Fatalf("events = %+v, want chunk + reply", sink.events)
	}
	for _, event := range sink.events {
		if event.SessionIncarnation != "incarnation-a" || event.TurnID != "turn-a" || event.Agent != "reviewer" {
			t.Fatalf("%s lost input identity: %+v", event.T, event)
		}
	}
}

func TestRunHubEmptyReplyStillTerminates(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "noop"}
	close(in)

	runHubTurns(context.Background(), in, sink, fakeBackend{reply: ""}, nil)

	if len(sink.events) == 0 || !reflect.DeepEqual(sink.events[len(sink.events)-1], contracts.Event{T: "reply", Done: true}) {
		t.Fatalf("empty reply must still emit a terminal reply{done}; got %+v", sink.events)
	}
}

// hangingBackend simulates a remote backend whose stream is lost mid-turn: the
// first Respond emits an event then errors (the streaming proxy surfaces a
// hangup that way); the second succeeds. It proves a stream loss ends the turn
// cleanly via the existing reply{done} path and the next queued input is still
// processed — the C3 abandonment guarantee, with no new abandonment path.
type hangingBackend struct{ calls int }

func (b *hangingBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	b.calls++
	if b.calls == 1 {
		onEvent(contracts.BackendEvent{Kind: "text", Detail: "partial"})
		return "", errors.New("backend: stream ended before reply{done}")
	}
	return "second ok", nil
}
func (*hangingBackend) Close() error { return nil }

func TestRunHubBackendStreamLossAbandonsTurnThenContinues(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 2)
	in <- contracts.Event{T: "input", Text: "first"}
	in <- contracts.Event{T: "input", Text: "second"}
	close(in)

	be := &hangingBackend{}
	runHubTurns(context.Background(), in, sink, be, nil)

	var dones []contracts.Event
	for _, e := range sink.events {
		if e.T == "reply" && e.Done {
			dones = append(dones, e)
		}
	}
	if len(dones) != 2 {
		t.Fatalf("want two terminal reply{done} (turn abandoned, then next processed); got %+v", sink.events)
	}
	if be.calls != 2 {
		t.Fatalf("second input must reach the recovered backend; got %d Respond calls", be.calls)
	}
	if dones[1].Text != "second ok" {
		t.Fatalf("second turn reply = %q, want %q", dones[1].Text, "second ok")
	}
}

func TestRunHubContextCancelStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	in := make(chan contracts.Event) // never fed
	done := make(chan struct{})
	go func() { runHubTurns(ctx, in, &recordSink{}, fakeBackend{}, nil); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runHubTurns did not return on cancelled context")
	}
}
