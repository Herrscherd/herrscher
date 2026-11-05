package manager

import (
	"context"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func (h *Handler) serviceRestartRun(ctx context.Context, _ contracts.Input) (contracts.Result, error) {
	if h.up == nil {
		return contracts.Result{}, ErrServiceControlUnavailable
	}
	if err := h.up.Restart(ctx); err != nil {
		return contracts.Result{}, fmt.Errorf("restart: %w", err)
	}
	return contracts.DataResult("🔄 Restarting the daemon…"), nil
}

func (h *Handler) serviceUpdateRun(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	if h.up == nil {
		return contracts.Result{}, ErrServiceControlUnavailable
	}
	pull := !input.Bool("no_pull")
	version, err := h.up.Build(ctx, pull)
	if err != nil {
		return contracts.Result{}, fmt.Errorf("update: %w", err)
	}
	if err := h.up.Restart(ctx); err != nil {
		return contracts.Result{}, fmt.Errorf("rebuilt %s but restart failed: %w", version, err)
	}
	reported := version
	if reported == "" {
		reported = "(unknown)"
	}
	return contracts.DataResult(fmt.Sprintf("✅ Rebuilt %#q, restarting the daemon…", reported)), nil
}
