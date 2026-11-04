// Package contextwindow answers how many tokens a vendor's context window holds.
package contextwindow

import (
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

// Limit returns the context window size for a vendor, HERRSCHER_CONTEXT_LIMIT overriding it when positive.
func Limit(vendor string) int {
	if v := os.Getenv(envLimit); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
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
