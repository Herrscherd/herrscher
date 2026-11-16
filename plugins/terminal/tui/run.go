package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"
)

func newWindow(ctx context.Context, backend Backend, opts []Option) *model {
	model := newRunModel(ctx, backend)
	for _, opt := range opts {
		opt(model)
	}
	return model
}

// NewWindow returns the window Run starts, for a host that drives the Bubble Tea
// program itself. The host feeds the backend's routed events to it as Event messages.
func NewWindow(ctx context.Context, backend Backend, opts ...Option) tea.Model {
	return newWindow(ctx, backend, opts)
}

// Event wraps a routed event of the backend as the message the window renders.
func Event(routed RoutedEvent) tea.Msg {
	return eventMsg(routed)
}

func killedByContext(ctx context.Context, err error) bool {
	return ctx.Err() != nil && errors.Is(err, tea.ErrProgramKilled)
}

func settleRun(ctx context.Context, cancel context.CancelFunc, runErr error) error {
	if killedByContext(ctx, runErr) {
		runErr = nil
	}
	cancel()
	return runErr
}

func runProgram(ctx context.Context, cancel context.CancelFunc, backend Backend, model *model, extra ...tea.ProgramOption) error {
	options := []tea.ProgramOption{
		tea.WithContext(ctx),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithReportFocus(),
	}
	program := tea.NewProgram(model, append(options, extra...)...)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case entry, ok := <-backend.Frontend():
				if !ok {
					return
				}
				program.Send(eventMsg(entry))
			}
		}
	}()
	_, runErr := program.Run()
	return settleRun(ctx, cancel, runErr)
}
