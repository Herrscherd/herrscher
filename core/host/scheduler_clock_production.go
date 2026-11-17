//go:build !e2e

package host

import "time"

func schedulerClock() (func() time.Time, error) {
	return time.Now, nil
}

func schedulerInterval() (time.Duration, error) {
	return schedulerTick, nil
}
