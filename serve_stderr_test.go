//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRedirectStderrCapturesDirectWrites proves the fix for the TUI corruption
// bug: a library that writes straight to os.Stderr (as the discord gateway does
// via fmt.Fprintf(os.Stderr, ...)) must land in the log file, not on the
// terminal the foreground TUI owns. The redirect works at the fd level, so even
// a direct os.Stderr write — not routed through our slog logger — is captured.
func TestRedirectStderrCapturesDirectWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.log")
	restore, err := redirectStderr(path)
	if err != nil {
		t.Fatalf("redirectStderr: %v", err)
	}
	fmt.Fprint(os.Stderr, "discord gateway: command sync: discord 401: Unauthorized\n")
	restore()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(b), "discord 401: Unauthorized") {
		t.Fatalf("direct os.Stderr write was not captured; log = %q", b)
	}
}
