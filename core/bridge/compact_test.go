package bridge

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/compactwire"
)

type compactingBackend struct {
	fakeBackend
}

func (compactingBackend) Compact(_ context.Context, _ contracts.CompactRequest, _ func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	return contracts.CompactResult{TokensAfter: 7, TokensMeasured: true, Note: "ok"}, nil
}

type collectSink struct {
	events []contracts.Event
}

func (s *collectSink) Emit(event contracts.Event) {
	s.events = append(s.events, event)
}

func compactFrame(t *testing.T) contracts.Event {
	t.Helper()
	frame, err := compactwire.Request(contracts.CompactRequest{
		Trigger:       contracts.TriggerManual,
		UsageMeasured: true,
		TokensUsed:    10,
		TokenLimit:    100,
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	return frame
}

func TestRunCompactReturnsTheBackendResult(t *testing.T) {
	sink := &collectSink{}
	runCompact(context.Background(), sink, compactingBackend{}, nil, compactFrame(t))
	if len(sink.events) != 1 {
		t.Fatalf("events = %+v, want one result", sink.events)
	}
	got := compactwire.DecodeResult(sink.events[0])
	if got.TokensAfter != 7 || !got.TokensMeasured || got.Note != "ok" {
		t.Fatalf("result = %+v, want 7 measured ok", got)
	}
}

func TestRunCompactRefusesABackendWithoutThePort(t *testing.T) {
	sink := &collectSink{}
	runCompact(context.Background(), sink, fakeBackend{}, nil, compactFrame(t))
	if len(sink.events) != 1 || sink.events[0].Value != compactwire.NoCompactor {
		t.Fatalf("events = %+v, want one no-port failure", sink.events)
	}
}
