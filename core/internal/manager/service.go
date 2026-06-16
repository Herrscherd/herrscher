package manager

import (
	"context"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func (h *Handler) serviceRestartRun(ctx context.Context, _ contracts.Input) (string, error) {
	if h.up == nil {
		return "", fmt.Errorf("service control unavailable")
	}
	if err := h.up.Restart(ctx); err != nil {
		return "", fmt.Errorf("restart: %v", err)
	}
	return "🔄 Restarting the daemon…", nil
}

func (h *Handler) serviceUpdateRun(ctx context.Context, in contracts.Input) (string, error) {
	if h.up == nil {
		return "", fmt.Errorf("service control unavailable")
	}
	pull := !in.Bool("no_pull")
	version, err := h.up.Build(ctx, pull)
	if err != nil {
		return "", fmt.Errorf("update: %v", err)
	}
	if err := h.up.Restart(ctx); err != nil {
		return "", fmt.Errorf("rebuilt %s but restart failed: %v", version, err)
	}
	v := version
	if v == "" {
		v = "(unknown)"
	}
	return fmt.Sprintf("✅ Rebuilt `%s`, restarting the daemon…", v), nil
}
