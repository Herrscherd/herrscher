package manager

import "context"

// SelfToolsProvisioner declares the herrscher MCP server in a new session's worktree.
type SelfToolsProvisioner func(ctx context.Context, worktree string) error

// SetSelfTools wires what declares the self tools in each local session worktree. Without it no session gets them.
func (h *Handler) SetSelfTools(provision SelfToolsProvisioner) { h.selfTools = provision }

func (h *Handler) provisionSelfTools(ctx context.Context, req createRequest, space workspace) {
	if h.selfTools == nil || req.hostName != "" || req.shared || space.path == "" {
		return
	}
	if err := h.selfTools(ctx, space.path); err != nil {
		h.log.Warn("self tools not fully materialized", "session", req.name, "err", err)
	}
}
