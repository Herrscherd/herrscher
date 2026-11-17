//go:build e2e

package host

import (
	"errors"
	"testing"
	"time"
)

func TestSchedulerClockReadsTheInjectedInstant(t *testing.T) {
	t.Setenv(envSchedulerClock, "2030-01-01T09:00:00Z")
	clock, err := schedulerClock()
	if err != nil {
		t.Fatalf("schedulerClock() error = %v, want none", err)
	}
	if want := time.Date(2030, 1, 1, 9, 0, 0, 0, time.UTC); !clock().Equal(want) {
		t.Fatalf("clock() = %v, want %v", clock(), want)
	}
}

func TestSchedulerClockRefusesAMalformedInstant(t *testing.T) {
	t.Setenv(envSchedulerClock, "tomorrow at nine")
	_, err := schedulerClock()
	parseErr, ok := errors.AsType[*time.ParseError](err)
	if !ok || parseErr.Value != "tomorrow at nine" {
		t.Fatalf("schedulerClock() error = %v, want a *time.ParseError on the malformed instant", err)
	}
}

func TestSchedulerIntervalReadsTheInjectedSpan(t *testing.T) {
	t.Setenv(envSchedulerTick, "20ms")
	span, err := schedulerInterval()
	if err != nil {
		t.Fatalf("schedulerInterval() error = %v, want none", err)
	}
	if span != 20*time.Millisecond {
		t.Fatalf("schedulerInterval() = %v, want 20ms", span)
	}
}

func TestSchedulerIntervalRefusesAMalformedSpan(t *testing.T) {
	t.Setenv(envSchedulerTick, "soon")
	_, err := schedulerInterval()
	if err == nil {
		t.Fatal("schedulerInterval() error = nil, want a refusal of the malformed span")
	}
	if _, nonPositive := errors.AsType[nonPositiveSpanError](err); nonPositive {
		t.Fatalf("schedulerInterval() error = %v, want a parse error, not a non-positive span", err)
	}
}

func TestSchedulerIntervalRefusesANonPositiveSpan(t *testing.T) {
	for _, raw := range []string{"0s", "-5s"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv(envSchedulerTick, raw)
			_, err := schedulerInterval()
			spanErr, ok := errors.AsType[nonPositiveSpanError](err)
			if !ok {
				t.Fatalf("schedulerInterval() error = %v, want a nonPositiveSpanError", err)
			}
			if spanErr.name != envSchedulerTick || spanErr.raw != raw {
				t.Fatalf("nonPositiveSpanError = %+v, want name %s and raw %q", spanErr, envSchedulerTick, raw)
			}
		})
	}
}
