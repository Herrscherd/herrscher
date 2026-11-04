// Package contextwindow answers how many tokens a vendor's context window holds.
package contextwindow

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	standard = 200_000
	fallback = 200_000
	envLimit = "HERRSCHER_CONTEXT_LIMIT"
)

func vendorLimit(vendor string) (int, bool) {
	switch strings.ToLower(vendor) {
	case "claude", "codex", "cursor":
		return standard, true
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

// Limits is the set of window sizes one process answers with: the environment
// override, then the per-vendor table of config.json, then the built-in sizes.
// The zero value answers with the built-in sizes alone.
type Limits struct {
	override  int
	perVendor map[string]int
}

// NewLimits builds the window sizes from the raw HERRSCHER_CONTEXT_LIMIT value and
// the contextLimits table. An empty or invalid environment value is ignored, as
// CheckOverride reports it; the table is validated when config.json is loaded.
func NewLimits(envRaw string, perVendor map[string]int) Limits {
	limits := Limits{perVendor: make(map[string]int, len(perVendor))}
	if envRaw != "" {
		if n, err := parseOverride(envRaw); err == nil {
			limits.override = n
		}
	}
	for vendor, size := range perVendor {
		limits.perVendor[strings.ToLower(vendor)] = size
	}
	return limits
}

// Limit returns the context window size for a vendor.
func (l Limits) Limit(vendor string) int {
	if l.override > 0 {
		return l.override
	}
	if size, ok := l.perVendor[strings.ToLower(vendor)]; ok {
		return size
	}
	if size, ok := vendorLimit(vendor); ok {
		return size
	}
	return fallback
}

// Fallback is the window size assumed for a vendor the table does not know.
func Fallback() int { return fallback }

// EnvVar names the environment variable that overrides the window size.
func EnvVar() string { return envLimit }
