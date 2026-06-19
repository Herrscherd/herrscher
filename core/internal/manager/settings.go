package manager

import (
	"context"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// setHomeRun points the daemon at the category/forum that holds session
// channels. The channel kind is resolved through the gateway so an invalid
// target (a plain text channel) is rejected before it is persisted.
func (h *Handler) setHomeRun(ctx context.Context, in contracts.Input) (string, error) {
	id, ok := in.Lookup("channel")
	if !ok || id == "" {
		return "", fmt.Errorf("missing channel")
	}
	kind, err := h.d.Kind(ctx, id)
	if err != nil {
		return "", fmt.Errorf("inspect channel: %v", err)
	}
	if kind != "category" && kind != "forum" {
		return "", fmt.Errorf("channel %s is %q — home must be a category or forum", id, kind)
	}
	if err := h.st.SetHome(state.HomeRef{ID: id, Type: kind}); err != nil {
		return "", fmt.Errorf("persist: %v", err)
	}
	return fmt.Sprintf("🏠 Home set to <#%s> (%s).", id, kind), nil
}

// setSourceRun records the source checkout `/service update` builds from.
func (h *Handler) setSourceRun(_ context.Context, in contracts.Input) (string, error) {
	path, ok := in.Lookup("path")
	if !ok || path == "" {
		return "", fmt.Errorf("missing path")
	}
	if err := h.st.SetSource(path); err != nil {
		return "", fmt.Errorf("persist: %v", err)
	}
	return fmt.Sprintf("📦 Source checkout set to `%s`.", path), nil
}
