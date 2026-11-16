//go:build e2e

package host

import (
	"strings"
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
	if err == nil || !strings.Contains(err.Error(), envSchedulerClock) {
		t.Fatalf("schedulerClock() error = %v, want one naming %s", err, envSchedulerClock)
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

func TestSchedulerIntervalRefusesAMalformedOrNonPositiveSpan(t *testing.T) {
	for _, raw := range []string{"soon", "0s", "-5s"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv(envSchedulerTick, raw)
			_, err := schedulerInterval()
			if err == nil || !strings.Contains(err.Error(), envSchedulerTick) {
				t.Fatalf("schedulerInterval() error = %v, want one naming %s", err, envSchedulerTick)
			}
		})
	}
}
