package host

import (
	"fmt"
	"time"
)

// EnvSeedTimeout overrides the default cap on a one-shot seed turn for every
// seed that does not name its own.
const EnvSeedTimeout = "HERRSCHER_SEED_TIMEOUT"

// resolveSeedTimeout settles how long a seed turn may run: the command's own
// --timeout first, then the environment, then zero — which means "say nothing"
// and leaves seedTurnTimeout in force.
//
// Zero and negative durations are refused rather than clamped. A cap of 0s reads
// as "no limit" to whoever typed it and would in fact cancel the turn before the
// backend answers, which is the opposite; an operator deserves to hear that at
// the point they typed it, not as a turn that dies instantly.
func resolveSeedTimeout(raw string, getenv func(string) string) (time.Duration, error) {
	source, value := "--timeout", raw
	if value == "" {
		source, value = EnvSeedTimeout, getenv(EnvSeedTimeout)
	}
	if value == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a duration (try 90s or 30m)", source, value)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive, got %s", source, d)
	}
	return d, nil
}
