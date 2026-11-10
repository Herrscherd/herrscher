package host

import (
	"log/slog"
	"time"
)

// EnvLimitsTimeout overrides how long a reading of an account's limits may take before
// it is reported failed.
const EnvLimitsTimeout = "HERRSCHER_LIMITS_TIMEOUT"

func limitsTimeout(getenv func(string) string) time.Duration {
	value := getenv(EnvLimitsTimeout)
	if value == "" {
		return 0
	}
	duration, err := time.ParseDuration(value)
	switch {
	case err != nil:
		slog.Warn("usage: limits timeout ignored", "err", &invalidDurationError{Source: EnvLimitsTimeout, Value: value})
		return 0
	case duration <= 0:
		slog.Warn("usage: limits timeout ignored", "err", &nonPositiveDurationError{Source: EnvLimitsTimeout, Duration: duration})
		return 0
	}
	return duration
}
