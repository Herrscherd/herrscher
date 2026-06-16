package manager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func (h *Handler) handleSet(ctx context.Context, in contracts.Command) contracts.CommandResponse {
	sub, _ := in.Data.Subcommand()
	switch sub {
	case "home":
		return h.setHome(ctx, in)
	case "workspace":
		return h.setWorkspace(in)
	case "source":
		return h.setSource(in)
	default:
		return errf("unknown /set subcommand")
	}
}

func (h *Handler) setHome(ctx context.Context, in contracts.Command) contracts.CommandResponse {
	id, ok := in.Data.Opt("channel")
	if !ok {
		return errf("missing channel")
	}
	kind, err := h.d.Kind(ctx, id)
	if err != nil {
		return errf("cannot read channel: %v", err)
	}
	var typ string
	switch kind {
	case "category":
		typ = "category"
	case "forum":
		typ = "forum"
	default:
		return errf("home must be a category or a forum")
	}
	if err := h.st.SetHome(state.HomeRef{ID: id, Type: typ}); err != nil {
		return errf("save failed: %v", err)
	}
	return contracts.CommandResponse{Content: fmt.Sprintf("🏠 Home set to %s `%s`.", typ, id), Private: true}
}

func (h *Handler) setWorkspace(in contracts.Command) contracts.CommandResponse {
	p, ok := in.Data.Opt("path")
	if !ok || p == "" {
		return errf("missing path")
	}
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return errf("bad path: %v", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return errf("not a directory: %s", abs)
	}
	if err := h.st.SetWorkspace(abs); err != nil {
		return errf("save failed: %v", err)
	}
	return contracts.CommandResponse{Content: fmt.Sprintf("📂 Workspace set to `%s`.", abs), Private: true}
}

// setSource records the dctl source checkout that /service update builds from.
func (h *Handler) setSource(in contracts.Command) contracts.CommandResponse {
	p, ok := in.Data.Opt("path")
	if !ok || p == "" {
		return errf("missing path")
	}
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return errf("bad path: %v", err)
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return errf("not a directory: %s", abs)
	}
	// A herrscher checkout has go.mod declaring module github.com/Herrscherd/herrscher;
	// reject anything else so a later /service update never runs `go build` in an
	// unrelated tree.
	if !isHerrscherCheckout(abs) {
		return errf("not a herrscher source checkout: %s", abs)
	}
	if err := h.st.SetSource(abs); err != nil {
		return errf("save failed: %v", err)
	}
	return contracts.CommandResponse{Content: fmt.Sprintf("🛠️ Source set to `%s`.", abs), Private: true}
}

// isHerrscherCheckout reports whether dir holds the herrscher module's go.mod.
func isHerrscherCheckout(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "module github.com/Herrscherd/herrscher" {
			return true
		}
	}
	return false
}
