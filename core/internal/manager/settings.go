package manager

import (
	"context"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func (h *Handler) setHomeRun(ctx context.Context, input contracts.Input) (string, error) {
	channelID, ok := input.Lookup("channel")
	if !ok || channelID == "" {
		return "", ErrMissingChannel
	}
	kind, err := h.d.Kind(ctx, channelID)
	if err != nil {
		return "", fmt.Errorf("inspect channel: %w", err)
	}
	if kind != categoryKind && kind != forumKind {
		return "", &ChannelKindError{Channel: channelID, Kind: kind, Need: "home must be a category or forum"}
	}
	if err := h.st.SetHome(state.HomeRef{ID: channelID, Type: kind}); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	return fmt.Sprintf("🏠 Home set to <#%s> (%s).", channelID, kind), nil
}

func (h *Handler) setSourceRun(_ context.Context, input contracts.Input) (string, error) {
	path, ok := input.Lookup("path")
	if !ok || path == "" {
		return "", ErrMissingPath
	}
	if err := h.st.SetSource(path); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	return fmt.Sprintf("📦 Source checkout set to %#q.", path), nil
}
