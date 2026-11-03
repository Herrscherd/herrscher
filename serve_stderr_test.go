//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedirectStderrCapturesDirectWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.log")
	restore, err := redirectStderr(path)
	if err != nil {
		t.Fatalf("redirectStderr: %v", err)
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
