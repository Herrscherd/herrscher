// Package obs is the operator-observability seam. It builds a single structured
// logger (stdlib log/slog) whose level is controlled by the -v flag and the
// HERRSCHER_LOG env, so every diagnostic line carries a level and stable fields
// (session, category, component) instead of an interpolated string. This is
// operator logging on stderr only — user-facing gateway rendering is untouched.
package obs

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// EnvLevel is the env var that selects the operator log level when -v is unset.
const EnvLevel = "HERRSCHER_LOG"

// ParseLevel resolves the operator log level. A set -v flag forces debug;
// otherwise the value (debug|info|warn|error, case/space-insensitive) decides,
// defaulting to info for empty or unrecognized input.
func ParseLevel(verbose bool, env string) slog.Level {
	if verbose {
		return slog.LevelDebug
	}
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// NewLogger builds a text-handler logger writing to w at the given level.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// Stderr builds the default operator logger: a text handler to stderr at the
// level resolved from the -v flag and HERRSCHER_LOG.
func Stderr(verbose bool) *slog.Logger {
	return NewLogger(os.Stderr, ParseLevel(verbose, os.Getenv(EnvLevel)))
}
