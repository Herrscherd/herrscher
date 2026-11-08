package bridge

import (
	"context"
	"errors"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/historywire"
	"github.com/Herrscherd/herrscher/core/internal/lease"
)

type editingBackend struct {
	fakeBackend

	pruned *[]contracts.HistoryEdit
}

func (editingBackend) ReadHistory(context.Context) (contracts.History, error) {
	return contracts.History{Messages: []contracts.HistoryMessage{{Role: contracts.RoleUser, Text: "bonjour"}}}, nil
}

func (b editingBackend) PruneHistory(_ context.Context, edits []contracts.HistoryEdit) (contracts.PruneOutcome, error) {
	*b.pruned = append(*b.pruned, edits...)
	return contracts.PruneOutcome{Edited: len(edits), CharsBefore: 100, CharsAfter: 10}, nil
}

func pruneFrame(t *testing.T, edits ...contracts.HistoryEdit) contracts.Event {
	t.Helper()
	frame, err := historywire.Prune("p1", edits)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	return frame
}

func TestRunHistoryReadAnswersFromALeasedBackend(t *testing.T) {
	sink := &collectSink{}
	runHistoryRead(context.Background(), sink, lease.Wrap(editingBackend{pruned: new([]contracts.HistoryEdit)}, func() {}), nil, historywire.Read("r1"))
	if len(sink.events) != 1 || sink.events[0].Who != "r1" {
		t.Fatalf("events = %+v, want one history answering r1", sink.events)
	}
	history, err := historywire.DecodeHistory(sink.events)
	if err != nil {
		t.Fatalf("DecodeHistory: %v", err)
	}
	if len(history.Messages) != 1 || history.Messages[0].Text != "bonjour" {
		t.Fatalf("history = %+v, want the backend's message", history)
	}
}

func TestRunHistoryPruneAppliesValidEdits(t *testing.T) {
	var pruned []contracts.HistoryEdit
	sink := &collectSink{}
	runHistoryPrune(context.Background(), sink, editingBackend{pruned: &pruned}, nil, pruneFrame(t, contracts.HistoryEdit{CallID: "c1", Action: contracts.PruneCall}))
	if len(sink.events) != 1 || sink.events[0].T != historywire.EventPruned || sink.events[0].Who != "p1" {
		t.Fatalf("events = %+v, want one pruned outcome answering p1", sink.events)
	}
	outcome, err := historywire.DecodePruned(sink.events[0])
	if err != nil {
		t.Fatalf("DecodePruned: %v", err)
	}
	if outcome.Edited != 1 || len(pruned) != 1 || pruned[0].CallID != "c1" {
		t.Fatalf("outcome = %+v, pruned = %+v, want the one edit applied", outcome, pruned)
	}
}

func TestRunHistoryPruneValidatesBeforeTheBackend(t *testing.T) {
	var pruned []contracts.HistoryEdit
	sink := &collectSink{}
	twice := contracts.HistoryEdit{CallID: "c1", Action: contracts.PruneCall}
	runHistoryPrune(context.Background(), sink, editingBackend{pruned: &pruned}, nil, pruneFrame(t, twice, twice))
	if len(sink.events) != 1 || sink.events[0].T != historywire.EventError {
		t.Fatalf("events = %+v, want one failure", sink.events)
	}
	if err := historywire.DecodeFailure(sink.events[0]); !errors.Is(err, contracts.ErrInvalidHistoryEdit) {
		t.Fatalf("failure = %v, want ErrInvalidHistoryEdit", err)
	}
	if len(pruned) != 0 {
		t.Fatalf("the backend received %+v, want no edit after a failed validation", pruned)
	}
}

func TestRunHistoryRefusesABackendWithoutThePort(t *testing.T) {
	sink := &collectSink{}
	runHistoryRead(context.Background(), sink, fakeBackend{}, nil, historywire.Read("r1"))
	runHistoryPrune(context.Background(), sink, fakeBackend{}, nil, pruneFrame(t, contracts.HistoryEdit{CallID: "c1", Action: contracts.PruneCall}))
	if len(sink.events) != 2 {
		t.Fatalf("events = %+v, want two refusals", sink.events)
	}
	for _, event := range sink.events {
		if event.T != historywire.EventError || historywire.CodeOf(event) != historywire.FailureNoEditor {
			t.Fatalf("event = %+v, want a no-port failure", event)
		}
	}
}
