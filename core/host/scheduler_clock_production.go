//go:build !e2e

package host

import "time"

func schedulerClock() func() time.Time {
	return time.Now
}

func schedulerInterval() time.Duration {
	return schedulerTick
}
