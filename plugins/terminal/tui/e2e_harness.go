//go:build e2e

package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Herrscherd/herrscher/core/clitext"
)

// NewWindow returns the window Run builds, for the end-to-end harness that runs the program itself.
func NewWindow(ctx context.Context, messages *clitext.Messages, backend Backend, opts ...Option) tea.Model {
	return newWindow(ctx, messages, backend, opts)
}

// Pump forwards the backend's routed events to send until ctx ends or the backend closes its stream.
func Pump(ctx context.Context, backend Backend, send func(tea.Msg)) {
	pump(ctx, backend, send)
}
