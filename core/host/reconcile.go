package host

import (
	"context"
	"errors"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func (h *hub) reconcile(ctx context.Context) retiredBatch {
	h.forgetGoneRefusals()
	persisted := make(map[string]state.Session)
	sessions := h.st.SnapshotSessions()
	for idx := range sessions {
		s := sessions[idx]
		persisted[s.Name] = s
	}
	h.mu.Lock()
	liveNames := make([]string, 0, len(h.live))
	for n := range h.live {
		liveNames = append(liveNames, n)
	}
	h.mu.Unlock()
	window, cancel := h.newGrace()
	batch := retiredBatch{window: window, cancel: cancel}
	for _, name := range liveNames {
		s, ok := persisted[name]
		if !ok || s.Archived {
			h.goDeadWithin(name, window)
		}
	}
	for name := range persisted {
		if persisted[name].Archived {
			continue
		}
		retired, err := h.reviveDeferred(ctx, name, window)
		batch.retired = append(batch.retired, retired...)
		if errors.Is(err, errHubClosed) {
			break
		}
	}
	return batch
}

func (h *hub) reconciling(ctx context.Context, run func() (contracts.Result, error)) (contracts.Result, error) {
	out, batch, err := h.runThenReconcile(ctx, run)
	batch.await(h)
	return out, err
}

func (h *hub) runThenReconcile(ctx context.Context, run func() (contracts.Result, error)) (contracts.Result, retiredBatch, error) {
	h.dispatchMu.Lock()
	defer h.dispatchMu.Unlock()
	out, err := run()
	return out, h.reconcile(ctx), err
}
