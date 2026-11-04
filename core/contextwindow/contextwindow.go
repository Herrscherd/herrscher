// Package contextwindow answers how many tokens a vendor's context window holds.
package contextwindow

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	standard = 200_000
	large    = 400_000
	fallback = 200_000
	envLimit = "HERRSCHER_CONTEXT_LIMIT"
)

func vendorLimit(vendor string) (int, bool) {
	switch strings.ToLower(vendor) {
	case "claude", "cursor":
		return standard, true
	case "codex":
		return large, true
	}
	return 0, false
}

var errInvalidOverride = errors.New("not a positive integer")

func parseOverride(raw string) (int, error) {
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, errInvalidOverride
	}
	return limit, nil
}

// CheckOverride reports why a raw HERRSCHER_CONTEXT_LIMIT value cannot be used, nil when it is empty or valid.
func CheckOverride(raw string) error {
	if raw == "" {
		return nil
	}
	if _, err := parseOverride(raw); err != nil {
		return fmt.Errorf("%q: %w", raw, err)
	}
	return nil
}

// Limit returns the context window size for a vendor, HERRSCHER_CONTEXT_LIMIT overriding it when positive.
func Limit(vendor string) int {
	if raw := os.Getenv(envLimit); raw != "" {
		if n, err := parseOverride(raw); err == nil {
			return n
		}
	}
	if n, ok := vendorLimit(vendor); ok {
		return n
	}
	return fallback
}

// Fallback is the window size assumed for a vendor the table does not know.
func Fallback() int { return fallback }

// EnvVar names the environment variable that overrides the window size.
func EnvVar() string { return envLimit }
