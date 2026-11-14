package bridge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestAFailedTurnHandsItsCauseBack(t *testing.T) {
	sink := &recordSink{}
	err := runOneTurn(context.Background(), sink, errBackend{}, nil, contracts.Event{T: "input", Text: "go"}, &turnController{}, nil, affordances{}, nil, 0)
	failed, ok := errors.AsType[*TurnFailedError](err)
	if !ok || failed.Err.Error() != "backend exploded" {
		t.Fatalf("runOneTurn error = %v, want the backend failure", err)
	}
}

func TestASuccessfulTurnHandsBackNoError(t *testing.T) {
	sink := &recordSink{}
	err := runOneTurn(context.Background(), sink, fakeBackend{reply: "done"}, nil, contracts.Event{T: "input", Text: "go"}, &turnController{}, nil, affordances{}, nil, 0)
	if err != nil {
		t.Fatalf("runOneTurn: %v", err)
	}
}

func TestRunOneShotReportsAFailedTurn(t *testing.T) {
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "go"}
	out := make(chan contracts.Event, 8)
	err := RunOneShot(context.Background(), func(string) (contracts.Backend, error) { return errBackend{}, nil }, nil, "channel", in, out)
	if _, ok := errors.AsType[*TurnFailedError](err); !ok {
		t.Fatalf("RunOneShot error = %v, want a TurnFailedError", err)
	}
}

func TestAFailedTurnKeepsWhatTheBackendStreamed(t *testing.T) {
	sink := &recordSink{}
	err := runOneTurn(context.Background(), sink, streamThenFailBackend{}, nil, contracts.Event{T: "input", Text: "go"}, &turnController{}, nil, affordances{}, nil, 0)
	failed, ok := errors.AsType[*TurnFailedError](err)
	if !ok {
		t.Fatalf("runOneTurn error = %v, want a TurnFailedError", err)
	}
	if got := failed.PartialOutput(); got != "partial answer" {
		t.Fatalf("PartialOutput() = %q, want the text streamed before the failure", got)
	}
}

func TestAFailedTurnJoinsItsDeltasAsTheBackendSentThem(t *testing.T) {
	sink := &recordSink{}
	backend := streamDeltasThenFailBackend{deltas: []string{"Je", " regarde", "."}}
	err := runOneTurn(context.Background(), sink, backend, nil, contracts.Event{T: "input", Text: "go"}, &turnController{}, nil, affordances{}, nil, 0)
	failed, ok := errors.AsType[*TurnFailedError](err)
	if !ok {
		t.Fatalf("runOneTurn error = %v, want a TurnFailedError", err)
	}
	if got, want := failed.PartialOutput(), "Je regarde."; got != want {
		t.Fatalf("PartialOutput() = %q, want the deltas concatenated as sent: %q", got, want)
	}
}

func TestAFailedTurnWithOnlyBlankDeltasHasNoPartialOutput(t *testing.T) {
	sink := &recordSink{}
	backend := streamDeltasThenFailBackend{deltas: []string{"\n", "  ", "\n"}}
	err := runOneTurn(context.Background(), sink, backend, nil, contracts.Event{T: "input", Text: "go"}, &turnController{}, nil, affordances{}, nil, 0)
	failed, ok := errors.AsType[*TurnFailedError](err)
	if !ok {
		t.Fatalf("runOneTurn error = %v, want a TurnFailedError", err)
	}
	if got := failed.PartialOutput(); got != "" {
		t.Fatalf("PartialOutput() = %q, want no partial output for blank deltas", got)
	}
}

func TestAFailedTurnIsLoggedAsAWarning(t *testing.T) {
	logs := captureLog(t)
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: "input", Text: "go"}
	close(in)
	runHubTurns(context.Background(), in, &recordSink{}, errBackend{})
	if got := logs.String(); !strings.Contains(got, "level=WARN") || !strings.Contains(got, "backend turn failed") {
		t.Fatalf("a failed turn must be logged as a warning; got log %q", got)
	}
}

func TestAnInterruptedTurnIsNotLoggedAsAFailure(t *testing.T) {
	logs := captureLog(t)
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
	if got := logs.String(); strings.Contains(got, "backend turn failed") {
		t.Fatalf("an interrupted turn is the operator's choice, not a failure; got log %q", got)
	}
}

type streamDeltasThenFailBackend struct{ deltas []string }

func (b streamDeltasThenFailBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	for _, delta := range b.deltas {
		onEvent(contracts.BackendEvent{Kind: "text", Detail: delta})
	}
	return "", errors.New("refused by the vendor")
}
func (streamDeltasThenFailBackend) Close() error { return nil }

type streamThenFailBackend struct{}

func (streamThenFailBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	onEvent(contracts.BackendEvent{Kind: "text", Detail: "partial answer"})
	return "refused by the vendor", errors.New("refused by the vendor")
}
func (streamThenFailBackend) Close() error { return nil }
