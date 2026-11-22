package host

import (
	"context"
	"errors"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func (h *hub) reconcile(ctx context.Context) retiredBatch {
	persisted := make(map[string]state.Session)
	sessions := h.st.SnapshotSessions()
	for idx := range sessions {
		s := sessions[idx]
		persisted[s.Name] = s
	}
	if h.sessions != nil {
		h.sessions.revokeTokensExcept(func(name string) bool {
			s, ok := persisted[name]
			return ok && !s.Archived
		})
	}
	h.mu.Lock()
	liveNames := make([]string, 0, len(h.live))
	incarnations := make(map[string]string, len(h.live))
	for liveName, live := range h.live {
		liveNames = append(liveNames, liveName)
		incarnations[liveName] = live.incarnation
	}
	h.mu.Unlock()
	h.lifecycle.Lock()
	defer h.releaseLifecycle()
	batch := h.newBatch()
	h.forgetGoneRefusalsLocked()
	for _, name := range liveNames {
		s, ok := persisted[name]
		if !ok || s.Archived {
			batch.retired = append(batch.retired, h.goDeadLocked(name, batch.window)...)
			h.announceClosed(name, renamedTo(name, incarnations[name], persisted))
		}
	}
	for name := range persisted {
		if persisted[name].Archived {
			continue
		}
		retired, err := h.reviveLocked(ctx, name, batch.window)
		batch.retired = append(batch.retired, retired...)
		if errors.Is(err, errHubClosed) {
			break
		}
	}
	return batch
}

func renamedTo(name, incarnation string, persisted map[string]state.Session) string {
	if incarnation == "" {
		return ""
	}
	for other := range persisted {
		if sess := persisted[other]; other != name && !sess.Archived && sess.Incarnation == incarnation {
			return other
		}
	}
	return ""
}

func (h *hub) announceClosed(name, target string) {
	if h.eventPublisher == nil {
		return
	}
	closed := contracts.Event{T: eventwire.EventClosed}
	if target != "" {
		h.eventJournal().rename(name, target)
		closed.Value, closed.Text = eventwire.ClosedRenamed, target
	}
	h.eventPublisher(name, closed)
}

func (h *hub) startReconcile(ctx context.Context) {
	h.reconcileUnderDispatch(ctx).await(h)
}

func (h *hub) reconcileUnderDispatch(ctx context.Context) retiredBatch {
	h.dispatchMu.Lock()
	defer h.dispatchMu.Unlock()
	return h.reconcile(ctx)
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
