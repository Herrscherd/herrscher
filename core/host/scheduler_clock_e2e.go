//go:build e2e

package host

import (
	"errors"
	"fmt"
	"os"
	"time"
)

const (
	envSchedulerClock = "HERRSCHER_E2E_CLOCK"
	envSchedulerTick  = "HERRSCHER_E2E_SCHEDULER_TICK"
)

var errNonPositive = errors.New("non-positive")

func schedulerClock() (func() time.Time, error) {
	raw := os.Getenv(envSchedulerClock)
	if raw == "" {
		return time.Now, nil
	}
	fixed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("%s=%q: %w", envSchedulerClock, raw, err)
	}
	return func() time.Time { return fixed }, nil
}

func schedulerInterval() (time.Duration, error) {
	raw := os.Getenv(envSchedulerTick)
	if raw == "" {
		return schedulerTick, nil
	}
	span, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s=%q: %w", envSchedulerTick, raw, err)
	}
	if span <= 0 {
		return 0, fmt.Errorf("%s=%q: %w", envSchedulerTick, raw, errNonPositive)
	}
	return span, nil
}
