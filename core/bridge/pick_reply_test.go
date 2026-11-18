package bridge

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type pickingBackend struct{ contracts.Backend }

func (pickingBackend) InjectChoice(_ context.Context, value string) (string, error) {
	return "picked " + value, nil
}

type pickEventSink struct{ events []contracts.Event }

func (s *pickEventSink) Emit(e contracts.Event) { s.events = append(s.events, e) }

func TestRunPickEchoesTheTurnIDOfThePickEvent(t *testing.T) {
	sink := &pickEventSink{}

	runPick(context.Background(), sink, pickingBackend{}, "2", "turn-pick")

	if len(sink.events) != 1 {
		t.Fatalf("pick emitted %d events, want one reply", len(sink.events))
	}
	if reply := sink.events[0]; reply.TurnID != "turn-pick" || !reply.Done {
		t.Fatalf("pick reply = %+v, want the done reply carrying the pick's TurnID turn-pick", reply)
	}
}
