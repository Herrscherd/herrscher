package obs

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		name    string
		verbose bool
		env     string
		want    slog.Level
	}{
		{"default empty", false, "", slog.LevelInfo},
		{"debug", false, "debug", slog.LevelDebug},
		{"info", false, "info", slog.LevelInfo},
		{"warn", false, "warn", slog.LevelWarn},
		{"warning alias", false, "warning", slog.LevelWarn},
		{"error", false, "error", slog.LevelError},
		{"mixed case + space", false, "  Debug ", slog.LevelDebug},
		{"unknown falls back to info", false, "chatty", slog.LevelInfo},
		{"verbose forces debug over env", true, "error", slog.LevelDebug},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseLevel(tc.verbose, tc.env); got != tc.want {
				t.Fatalf("ParseLevel(%v, %q) = %v, want %v", tc.verbose, tc.env, got, tc.want)
			}
		})
	}
}

// TestLoggerLevelGates asserts the constructed logger emits at/above its level
// and drops records below it — the verbosity contract HERRSCHER_LOG controls.
func TestLoggerLevelGates(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, slog.LevelWarn)
	log.Info("info line", "session", "demo")
	log.Warn("warn line", "session", "demo")

	out := buf.String()
	if strings.Contains(out, "info line") {
		t.Fatalf("info record should be suppressed at warn level: %q", out)
	}
	if !strings.Contains(out, "warn line") {
		t.Fatalf("warn record should be emitted at warn level: %q", out)
	}
}

// TestLoggerEmitsStructuredFields asserts records are key=value structured, not
// interpolated strings: level and the session field are stable, parseable tokens.
func TestLoggerEmitsStructuredFields(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, slog.LevelDebug)
	log.Warn("bridge exited", "session", "demo")

	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Fatalf("expected level=WARN token, got %q", out)
	}
	if !strings.Contains(out, "session=demo") {
		t.Fatalf("expected session=demo field, got %q", out)
	}
}
