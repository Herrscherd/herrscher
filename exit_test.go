package main

import (
	"context"
	"fmt"
	"testing"
)

func TestASignalledStopExitsCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code := reportOutcome(ctx, fmt.Errorf("serve: %w", context.Canceled)); code != 0 {
		t.Fatalf("a stop asked by a signal exited %d, want 0", code)
	}
}

func TestACancellationWithoutASignalStillFails(t *testing.T) {
	if code := reportOutcome(t.Context(), fmt.Errorf("serve: %w", context.Canceled)); code != 1 {
		t.Fatalf("a cancellation the process did not ask for exited %d, want 1", code)
	}
}

func TestAnErrorAfterTheSignalStillFails(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code := reportOutcome(ctx, errUnknownCommand); code != 1 {
		t.Fatalf("a real failure during shutdown exited %d, want 1", code)
	}
}
