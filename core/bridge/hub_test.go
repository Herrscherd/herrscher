package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
)

type recordSink struct{ events []contracts.Event }

func (s *recordSink) Emit(e contracts.Event) { s.events = append(s.events, e) }

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

func TestRunHubReplyCarriesCost(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "go"}
	close(in)

	runHubTurns(context.Background(), in, sink, fakeBackend{reply: "ok", cost: 0.0042})

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
	if !slices.EqualFunc(sink.events, want, sameEvent) {
		t.Fatalf("emitted %+v, want %+v", sink.events, want)
	}
}

func TestEmitBackendEventPanels(t *testing.T) {
	todos := []contracts.TodoItem{{Text: "lire", State: "done"}}
	agent := contracts.Subagent{ID: "a1", Name: "chercher", Kind: "Explore", State: "active"}

	sink := &recordSink{}
	emitBackendEvent(sink, contracts.BackendEvent{Kind: "todos", Todos: todos}, 0, 0, 0, 0)
	emitBackendEvent(sink, contracts.BackendEvent{Kind: "subagent", Subagent: &agent}, 0, 0, 0, 0)
	emitBackendEvent(sink, contracts.BackendEvent{Kind: "subagent"}, 0, 0, 0, 0)

	want := []contracts.Event{{T: "todos", Todos: todos}, {T: "subagent", Subagent: &agent}}
	if !slices.EqualFunc(sink.events, want, sameEvent) {
		t.Fatalf("emitted %+v, want %+v", sink.events, want)
	}
}

type tokenBackend struct{ reply string }

func (b tokenBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	onEvent(contracts.BackendEvent{Kind: "usage", InTokens: 10, OutTokens: 20})
	onEvent(contracts.BackendEvent{Kind: "text", Detail: "working"})
	onEvent(contracts.BackendEvent{Kind: "result", InTokens: 30, OutTokens: 55, Cost: 0.001})
	return b.reply, nil
}
func (tokenBackend) Close() error { return nil }

func TestRunOneTurn_StampsTokens(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "go"}
	close(in)

	runHubTurns(context.Background(), in, sink, tokenBackend{reply: "done"})

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

type agenticBackend struct{ reply string }

func (b agenticBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	for range 3 {
		onEvent(contracts.BackendEvent{Kind: "usage", InTokens: 100, OutTokens: 20})
		onEvent(contracts.BackendEvent{Kind: "text", Detail: "step"})
	}
	onEvent(contracts.BackendEvent{Kind: "result", InTokens: 300, OutTokens: 60, Cost: 0.002})
	return b.reply, nil
}
func (agenticBackend) Close() error { return nil }

func TestRunOneTurn_AccumulatesUsageAcrossMessages(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "go"}
	close(in)

	runHubTurns(context.Background(), in, sink, agenticBackend{reply: "done"})

	var chunks []int
	for _, e := range sink.events {
		if e.T == "chunk" {
			chunks = append(chunks, e.Tokens)
		}
	}
	if want := []int{20, 40, 60}; !slices.Equal(chunks, want) {
		t.Fatalf("live output tokens = %v, want %v (a running sum)", chunks, want)
	}
	last := sink.events[len(sink.events)-1]
	if last.T != "reply" || last.Tokens != 60 || last.TokensIn != 300 {
		t.Fatalf("reply must carry the result's authoritative totals, not the sum; got %+v", last)
	}
}

type cacheBackend struct{ reply string }

func (b cacheBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	onEvent(contracts.BackendEvent{Kind: "usage", InTokens: 10, OutTokens: 20, CacheRead: 12, CacheCreate: 3})
	onEvent(contracts.BackendEvent{Kind: "text", Detail: "working"})
	onEvent(contracts.BackendEvent{Kind: "result", InTokens: 30, OutTokens: 55, CacheRead: 12, CacheCreate: 3, Cost: 0.001})
	return b.reply, nil
}
func (cacheBackend) Close() error { return nil }

func TestRunOneTurn_StampsTokenBreakdown(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "go"}
	close(in)

	runHubTurns(context.Background(), in, sink, cacheBackend{reply: "done"})

	last := sink.events[len(sink.events)-1]
	if last.T != "reply" || last.TokensIn != 30 || last.CacheRead != 12 || last.CacheCreate != 3 {
		t.Fatalf("reply must carry token breakdown; got %+v", last)
	}
}

type blockingBackend struct{ started chan struct{} }

func (b blockingBackend) Respond(ctx context.Context, _ contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	close(b.started)
	<-ctx.Done()
	return "", ctx.Err()
}
func (blockingBackend) Close() error { return nil }

func TestInbound_InterruptCancelsTurn(t *testing.T) {
	ctrl := &turnController{}
	sink := &recordSink{}
	in := make(chan contracts.Event)
	be := blockingBackend{started: make(chan struct{})}

	done := make(chan struct{})
	go func() {
		runHubTurnsCtl(context.Background(), in, sink, be, nil, ctrl, nil, affordances{}, nil)
		close(done)
	}()

	in <- contracts.Event{T: "input", Text: "go"}
	<-be.started
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
	if last.Text != interruptNotice {
		t.Fatalf("an interrupted turn must read as interrupted; got %q", last.Text)
	}
}

type partialBackend struct{ started chan struct{} }

func (b partialBackend) Respond(ctx context.Context, _ contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	close(b.started)
	<-ctx.Done()
	return "half a thought", ctx.Err()
}
func (partialBackend) Close() error { return nil }

func TestInterruptKeepsPartialOutput(t *testing.T) {
	ctrl := &turnController{}
	sink := &recordSink{}
	in := make(chan contracts.Event)
	be := partialBackend{started: make(chan struct{})}

	done := make(chan struct{})
	go func() {
		runHubTurnsCtl(context.Background(), in, sink, be, nil, ctrl, nil, affordances{}, nil)
		close(done)
	}()

	in <- contracts.Event{T: "input", Text: "go"}
	<-be.started
	ctrl.interrupt()
	close(in)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("interrupt did not cancel the in-flight turn")
	}

	last := sink.events[len(sink.events)-1]
	if !strings.Contains(last.Text, "half a thought") {
		t.Fatalf("partial output must survive the interrupt; got %q", last.Text)
	}
	if !strings.Contains(last.Text, interruptNotice) {
		t.Fatalf("a cut-short reply must say so; got %q", last.Text)
	}
}

func TestBackendFailureStillReadsAsAFailure(t *testing.T) {
	sink := &recordSink{}
	runOneTurn(context.Background(), sink, errBackend{}, nil, contracts.Event{T: "input", Text: "go"}, &turnController{}, nil, affordances{}, nil, 0)
	last := sink.events[len(sink.events)-1]
	if !strings.HasPrefix(last.Text, errPrefix) {
		t.Fatalf("an uninterrupted failure must keep its banner; got %q", last.Text)
	}
}

type errBackend struct{}

func (errBackend) Respond(context.Context, contracts.Prompt, func(contracts.BackendEvent)) (string, error) {
	return "", errors.New("backend exploded")
}
func (errBackend) Close() error { return nil }

func TestRunHubOneTurn(t *testing.T) {
	sink := &recordSink{}
	in := make(chan contracts.Event, 2)
	in <- contracts.Event{T: "input", Who: "alice", Text: "refactor"}
	close(in)

	be := fakeBackend{reply: "done · 4 files"}
	runHubTurns(context.Background(), in, sink, be)

	want := []contracts.Event{
		{T: "chunk", Text: "thinking"},
		{T: "reply", Text: "done · 4 files", Done: true},
	}
	if len(sink.events) != len(want) {
		t.Fatalf("emitted %+v, want %+v", sink.events, want)
	}
	for i := range want {
		if !sameEvent(sink.events[i], want[i]) {
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

	runHubTurns(context.Background(), in, sink, fakeBackend{reply: "done"})

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

	runHubTurns(context.Background(), in, sink, fakeBackend{reply: ""})

	if len(sink.events) == 0 || !sameEvent(sink.events[len(sink.events)-1], contracts.Event{T: "reply", Done: true}) {
		t.Fatalf("empty reply must still emit a terminal reply{done}; got %+v", sink.events)
	}
}

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
	runHubTurns(context.Background(), in, sink, be)

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
	in := make(chan contracts.Event)
	done := make(chan struct{})
	go func() { runHubTurns(ctx, in, &recordSink{}, fakeBackend{}); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runHubTurns did not return on cancelled context")
	}
}

func sameEvent(a, b contracts.Event) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

func TestBridgeAnswersWithNoLanguageAfterAnEmptyLanguageFrame(t *testing.T) {
	in := make(chan contracts.Event, 4)
	be := &captureBackend{reply: "ok"}
	in <- contracts.Event{T: "language", Value: "fr"}
	in <- contracts.Event{T: "language", Value: ""}
	in <- contracts.Event{T: "input", Text: "hello"}
	close(in)

	runHubTurns(context.Background(), in, &recordSink{}, be)

	if want := []i18n.Language{0}; !slices.Equal(be.languages, want) {
		t.Fatalf("prompt languages = %v, want %v", be.languages, want)
	}
}

func TestRunOneShotAnswersOnlyTheInputAfterControlFrames(t *testing.T) {
	in := make(chan contracts.Event, 3)
	be := &captureBackend{reply: "ok"}
	in <- contracts.Event{T: "interrupt"}
	in <- contracts.Event{T: "language", Value: "fr"}
	in <- contracts.Event{T: "input", Text: "bonjour"}
	out := make(chan contracts.Event, 8)

	err := RunOneShot(context.Background(), func(string) (contracts.Backend, error) { return be, nil }, nil, "channel", in, out)
	if err != nil {
		t.Fatalf("RunOneShot: %v", err)
	}
	if want := []string{"bonjour"}; !slices.Equal(be.texts, want) {
		t.Fatalf("turn texts = %q, want %q", be.texts, want)
	}
	if want := []i18n.Language{i18n.French}; !slices.Equal(be.languages, want) {
		t.Fatalf("turn languages = %v, want %v", be.languages, want)
	}
}

func TestBridgeIgnoresUnknownFrameTypes(t *testing.T) {
	in := make(chan contracts.Event, 3)
	be := &captureBackend{reply: "ok"}
	in <- contracts.Event{T: "mystery", Text: "not a turn"}
	in <- contracts.Event{T: "input", Text: "bonjour"}
	close(in)

	sink := &recordSink{}
	runHubTurns(context.Background(), in, sink, be)

	if got := len(be.contexts); got != 1 {
		t.Fatalf("turns answered = %d, want 1", got)
	}
}

func TestBridgeAnswersEachTurnInTheLanguageLastSet(t *testing.T) {
	in := make(chan contracts.Event, 4)
	be := &captureBackend{reply: "ok"}
	in <- contracts.Event{T: "language", Value: "fr"}
	in <- contracts.Event{T: "input", Text: "bonjour"}
	in <- contracts.Event{T: "language", Value: "en"}
	in <- contracts.Event{T: "input", Text: "hello"}
	close(in)

	runHubTurns(context.Background(), in, &recordSink{}, be)

	if want := []i18n.Language{i18n.French, i18n.English}; !slices.Equal(be.languages, want) {
		t.Fatalf("prompt languages = %v, want %v", be.languages, want)
	}
}
