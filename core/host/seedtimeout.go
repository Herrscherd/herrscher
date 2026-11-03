package host

import (
	"time"
)

// EnvSeedTimeout overrides the default cap on a one-shot seed turn for every
// seed that does not name its own.
const EnvSeedTimeout = "HERRSCHER_SEED_TIMEOUT"

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
		return 0, &invalidDurationError{Source: source, Value: value}
	}
	if d <= 0 {
		return 0, &nonPositiveDurationError{Source: source, Duration: d}
	}
	return d, nil
}
