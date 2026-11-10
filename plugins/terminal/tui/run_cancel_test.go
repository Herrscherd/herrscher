package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKilledByContextIsNotAFailure(t *testing.T) {
	otherErr := errors.New("boom")

	tests := []struct {
		name      string
		cancelled bool
		err       error
		want      bool
	}{
		{name: "quit by cancellation", cancelled: true, err: tea.ErrProgramKilled, want: true},
		{name: "killed while the context is live", cancelled: false, err: tea.ErrProgramKilled, want: false},
		{name: "cancelled with another failure", cancelled: true, err: otherErr, want: false},
		{name: "cancelled without error", cancelled: true, err: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if tt.cancelled {
				cancel()
			} else {
				t.Cleanup(cancel)
			}
			if got := killedByContext(ctx, tt.err); got != tt.want {
				t.Fatalf("killedByContext() = %t, want %t", got, tt.want)
			}
		})
	}
}
