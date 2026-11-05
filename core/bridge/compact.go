package bridge

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/compactwire"
)

func runCompact(ctx context.Context, sink contracts.EventSink, backend contracts.Backend, ctrl *turnController, event contracts.Event) {
	compactor, ok := compactorOf(backend)
	if !ok {
		sink.Emit(compactwire.NoPort())
		return
	}
	req, err := compactwire.DecodeRequest(event)
	if err != nil {
		sink.Emit(compactwire.Failure(err))
		return
	}
	turnCtx, endTurn := ctrl.begin(ctx)
	defer endTurn()
	result, err := compactor.Compact(turnCtx, req, func(progress contracts.BackendEvent) {
		if progress.Detail != "" {
			sink.Emit(compactwire.Progress(progress.Detail))
		}
	})
	if err != nil {
		sink.Emit(compactwire.Failure(err))
		return
	}
	sink.Emit(compactwire.Result(result))
}
