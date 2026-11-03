package main

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"testing"
)

func cancelledBy(t *testing.T, sig os.Signal) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(stopSignalError{sig: sig})
	return ctx
}

func TestATerminateStopExitsCleanly(t *testing.T) {
	if code := reportOutcome(cancelledBy(t, syscall.SIGTERM), fmt.Errorf("serve: %w", context.Canceled)); code != 0 {
		t.Fatalf("a stop asked by SIGTERM exited %d, want 0", code)
	}
}

func TestAnInterruptExitsAsInterrupted(t *testing.T) {
	if code := reportOutcome(cancelledBy(t, os.Interrupt), fmt.Errorf("turn: %w", context.Canceled)); code != exitInterrupted {
		t.Fatalf("an interrupted command exited %d, want %d", code, exitInterrupted)
	}
}

func TestACancellationWithoutASignalStillFails(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code := reportOutcome(ctx, fmt.Errorf("serve: %w", context.Canceled)); code != 1 {
		t.Fatalf("a cancellation no signal asked for exited %d, want 1", code)
	}
}

func TestAnErrorAfterTheSignalStillFails(t *testing.T) {
	if code := reportOutcome(cancelledBy(t, syscall.SIGTERM), errUnknownCommand); code != 1 {
		t.Fatalf("a real failure during shutdown exited %d, want 1", code)
	}
}
