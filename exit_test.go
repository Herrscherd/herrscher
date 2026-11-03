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

func TestADaemonStoppedByTerminateExitsCleanly(t *testing.T) {
	for _, daemon := range []string{"serve", "bridge"} {
		if code := outcomeReporterFor(daemon)(cancelledBy(t, syscall.SIGTERM), fmt.Errorf("%s: %w", daemon, context.Canceled)); code != 0 {
			t.Errorf("%s stopped by SIGTERM exited %d, want 0", daemon, code)
		}
	}
}

func TestAOneShotStoppedByTerminateStillFails(t *testing.T) {
	if code := reportOutcome(cancelledBy(t, syscall.SIGTERM), fmt.Errorf("turn: %w", context.Canceled)); code != exitTerminated {
		t.Fatalf("a one-shot killed by SIGTERM exited %d, want %d", code, exitTerminated)
	}
	if code := outcomeReporterFor("session")(cancelledBy(t, syscall.SIGTERM), fmt.Errorf("session: %w", context.Canceled)); code != exitTerminated {
		t.Fatalf("a non-daemon command killed by SIGTERM exited %d, want %d", code, exitTerminated)
	}
}

func TestADaemonInterruptedStillExitsAsInterrupted(t *testing.T) {
	if code := reportDaemonOutcome(cancelledBy(t, os.Interrupt), fmt.Errorf("serve: %w", context.Canceled)); code != exitInterrupted {
		t.Fatalf("an interrupted daemon exited %d, want %d", code, exitInterrupted)
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
	if code := reportDaemonOutcome(cancelledBy(t, syscall.SIGTERM), errUnknownCommand); code != 1 {
		t.Fatalf("a real failure during shutdown exited %d, want 1", code)
	}
}
