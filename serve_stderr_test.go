//go:build linux

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedirectStderrCapturesDirectWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.log")
	restore, err := redirectStderrCapped(path, maxLogFileBytes)
	if err != nil {
		t.Fatalf("redirectStderrCapped: %v", err)
	}
	fmt.Fprint(os.Stderr, "discord gateway: command sync: discord 401: Unauthorized\n")
	if err = restore(); err != nil {
		t.Fatalf("restore stderr: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(b), "discord 401: Unauthorized") {
		t.Fatalf("direct os.Stderr write was not captured; log = %q", b)
	}
}

func TestServeLogStaysBoundedWhileTheDaemonRuns(t *testing.T) {
	const limit = 512
	path := filepath.Join(t.TempDir(), "serve.log")
	restore, err := redirectStderrCapped(path, limit)
	if err != nil {
		t.Fatalf("redirectStderrCapped: %v", err)
	}
	line := strings.Repeat("d", 99) + "\n"
	fmt.Fprint(os.Stderr, strings.Repeat(line, 200))
	if err = restore(); err != nil {
		t.Fatalf("restore stderr: %v", err)
	}

	current, err := os.Stat(path)
	if err != nil || current.Size() > limit {
		t.Fatalf("serve.log = %v, %v; want at most %d bytes", current, err, limit)
	}
	previous, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("serve.log.1 missing after the daemon overflowed the cap: %v", err)
	}
	if len(previous) == 0 || int64(len(previous)) > limit {
		t.Fatalf("serve.log.1 = %d bytes, want one bounded generation between 1 and %d bytes", len(previous), limit)
	}
	currentBytes, err := os.ReadFile(path)
	if err != nil || !bytes.HasSuffix(currentBytes, []byte("dd\n")) {
		t.Fatalf("serve.log does not end with the tail of the last line written: %q, %v", currentBytes, err)
	}
}
