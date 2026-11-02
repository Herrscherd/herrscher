package manager

import (
	"context"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func (h *Handler) serviceRestartRun(ctx context.Context, _ contracts.Input) (string, error) {
	if h.up == nil {
		return "", ErrServiceControlUnavailable
	}
	if err := h.up.Restart(ctx); err != nil {
		return "", fmt.Errorf("restart: %w", err)
	}
	return "🔄 Restarting the daemon…", nil
}

func (h *Handler) serviceUpdateRun(ctx context.Context, in contracts.Input) (string, error) {
	if h.up == nil {
		return "", ErrServiceControlUnavailable
	}
	pull := !in.Bool("no_pull")
	version, err := h.up.Build(ctx, pull)
	if err != nil {
		return "", fmt.Errorf("update: %w", err)
	}
	if err := h.up.Restart(ctx); err != nil {
		return "", fmt.Errorf("rebuilt %s but restart failed: %w", version, err)
	}
	v := version
	if v == "" {
		v = "(unknown)"
	}
	return fmt.Sprintf("✅ Rebuilt %#q, restarting the daemon…", v), nil
}
