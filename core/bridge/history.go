package bridge

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/historywire"
)

func runHistoryRead(ctx context.Context, sink contracts.EventSink, backend contracts.Backend, ctrl *turnController) {
	editor, ok := historyEditorOf(backend)
	if !ok {
		sink.Emit(historywire.NoPort())
		return
	}
	readCtx, endRead := ctrl.begin(ctx)
	defer endRead()
	history, err := editor.ReadHistory(readCtx)
	if err != nil {
		sink.Emit(historywire.Failure(err))
		return
	}
	frame, err := historywire.History(history)
	if err != nil {
		sink.Emit(historywire.Failure(err))
		return
	}
	sink.Emit(frame)
}

func runHistoryPrune(ctx context.Context, sink contracts.EventSink, backend contracts.Backend, ctrl *turnController, event contracts.Event) {
	editor, ok := historyEditorOf(backend)
	if !ok {
		sink.Emit(historywire.NoPort())
		return
	}
	edits, err := historywire.DecodePrune(event)
	if err != nil {
		sink.Emit(historywire.Failure(err))
		return
	}
	if err = contracts.ValidateHistoryEdits(edits); err != nil {
		sink.Emit(historywire.Failure(err))
		return
	}
	pruneCtx, endPrune := ctrl.begin(ctx)
	defer endPrune()
	outcome, err := editor.PruneHistory(pruneCtx, edits)
	if err != nil {
		sink.Emit(historywire.Failure(err))
		return
	}
	frame, err := historywire.Pruned(outcome)
	if err != nil {
		sink.Emit(historywire.Failure(err))
		return
	}
	sink.Emit(frame)
}
