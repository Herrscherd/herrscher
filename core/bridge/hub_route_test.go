package bridge

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/compactwire"
	"github.com/Herrscherd/herrscher/core/historywire"
)

type compactingEditingBackend struct {
	editingBackend
}

func (compactingEditingBackend) Compact(context.Context, contracts.CompactRequest, func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	return contracts.CompactResult{TokensAfter: 7, TokensMeasured: true, Note: "ok"}, nil
}

func TestHubRoutesCompactionAndHistoryFrames(t *testing.T) {
	var pruned []contracts.HistoryEdit
	in := make(chan contracts.Event, 2)
	in <- compactFrame(t)
	in <- pruneFrame(t, contracts.HistoryEdit{CallID: "c1", Action: contracts.PruneCall})
	close(in)
	sink := &recordSink{}
	runHubTurnsCtl(context.Background(), in, sink, compactingEditingBackend{editingBackend{pruned: &pruned}}, nil, &turnController{}, nil, affordances{}, nil)
	var compacted, prunedAnswers int
	for _, event := range sink.events {
		switch event.T {
		case compactwire.EventResult:
			compacted++
		case historywire.EventPruned:
			prunedAnswers++
		}
	}
	if compacted != 1 {
		t.Fatalf("events = %+v, want one compaction result from the hub", sink.events)
	}
	if prunedAnswers != 1 || len(pruned) != 1 {
		t.Fatalf("events = %+v, pruned = %+v, want the hub to apply the prune and answer it", sink.events, pruned)
	}
}
