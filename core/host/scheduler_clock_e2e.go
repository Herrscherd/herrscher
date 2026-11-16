//go:build e2e

package host

import (
	"os"
	"time"
)

const (
	envSchedulerClock = "HERRSCHER_E2E_CLOCK"
	envSchedulerTick  = "HERRSCHER_E2E_SCHEDULER_TICK"
)

func schedulerClock() func() time.Time {
	fixed, err := time.Parse(time.RFC3339, os.Getenv(envSchedulerClock))
	if err != nil {
		return time.Now
	}
	return func() time.Time { return fixed }
}

func schedulerInterval() time.Duration {
	span, err := time.ParseDuration(os.Getenv(envSchedulerTick))
	if err != nil || span <= 0 {
		return schedulerTick
	}
	return span
}
