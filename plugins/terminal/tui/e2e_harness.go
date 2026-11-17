//go:build e2e

package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// NewWindow returns the window Run builds, for the end-to-end harness that runs the program itself.
func NewWindow(ctx context.Context, backend Backend, opts ...Option) tea.Model {
	return newWindow(ctx, backend, opts)
}

// Pump forwards the backend's routed events to send until ctx ends or the backend closes its stream.
func Pump(ctx context.Context, backend Backend, send func(tea.Msg)) {
	pump(ctx, backend, send)
}
