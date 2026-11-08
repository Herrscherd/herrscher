package bridge

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/historywire"
)

func runHistoryRead(ctx context.Context, sink contracts.EventSink, backend contracts.Backend, ctrl *turnController, request contracts.Event) {
	editor, ok := historyEditorOf(backend)
	if !ok {
		sink.Emit(historywire.Answer(request, historywire.NoPort()))
		return
	}
	readCtx, endRead := ctrl.begin(ctx)
	defer endRead()
	history, err := editor.ReadHistory(readCtx)
	if err != nil {
		sink.Emit(historywire.Answer(request, historywire.Failure(err)))
		return
	}
	frames, err := historywire.History(history)
	if err != nil {
		sink.Emit(historywire.Answer(request, historywire.Failure(err)))
		return
	}
	for i := range frames {
		sink.Emit(historywire.Answer(request, frames[i]))
	}
}

func runHistoryPrune(ctx context.Context, sink contracts.EventSink, backend contracts.Backend, ctrl *turnController, request contracts.Event) {
	editor, ok := historyEditorOf(backend)
	if !ok {
		sink.Emit(historywire.Answer(request, historywire.NoPort()))
		return
	}
	edits, err := historywire.DecodePrune(request)
	if err != nil {
		sink.Emit(historywire.Answer(request, historywire.Failure(err)))
		return
	}
	if err = contracts.ValidateHistoryEdits(edits); err != nil {
		sink.Emit(historywire.Answer(request, historywire.Failure(err)))
		return
	}
	pruneCtx, endPrune := ctrl.begin(ctx)
	defer endPrune()
	outcome, err := editor.PruneHistory(pruneCtx, edits)
	if err != nil {
		sink.Emit(historywire.Answer(request, historywire.Failure(err)))
		return
	}
	frame, err := historywire.Pruned(outcome)
	if err != nil {
		sink.Emit(historywire.Answer(request, historywire.Failure(err)))
		return
	}
	sink.Emit(historywire.Answer(request, frame))
}
