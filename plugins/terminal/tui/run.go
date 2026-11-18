package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func newWindow(ctx context.Context, messages *clitext.Messages, backend Backend, opts []Option) *model {
	model := newRunModel(ctx, messages, backend)
	for _, opt := range opts {
		opt(model)
	}
	return model
}

func pump(ctx context.Context, backend Backend, send func(tea.Msg)) {
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-backend.Frontend():
			if !ok {
				return
			}
			send(eventMsg(entry))
		}
	}
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
	go pump(ctx, backend, program.Send)
	_, runErr := program.Run()
	return settleRun(ctx, cancel, runErr)
}
