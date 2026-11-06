//go:build linux

package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientSessionKeepsStderrQuiet(t *testing.T) {
	dir := t.TempDir()
	terminalCapture := filepath.Join(dir, "terminal.txt")
	clientLog := filepath.Join(dir, "client.log")

	restoreTerminal, err := redirectStderr(terminalCapture)
	if err != nil {
		t.Fatalf("redirect terminal: %v", err)
	}
	sessionErr := withClientLog(clientLog, func() error {
		slog.Warn("transcript image dropped", "err", "decode failed")
		fmt.Fprintln(os.Stderr, "raw stderr write")
		return nil
	})
	if err = restoreTerminal(); err != nil {
		t.Fatalf("restore terminal: %v", err)
	}
	if sessionErr != nil {
		t.Fatalf("withClientLog: %v", sessionErr)
	}

	onTerminal, err := os.ReadFile(terminalCapture)
	if err != nil {
		t.Fatalf("read terminal capture: %v", err)
	}
	if len(onTerminal) != 0 {
		t.Fatalf("stderr reached the terminal during the session: %q", onTerminal)
	}

	logged, err := os.ReadFile(clientLog)
	if err != nil {
		t.Fatalf("read client log: %v", err)
	}
	for _, want := range []string{"transcript image dropped", "decode failed", "raw stderr write"} {
		if !strings.Contains(string(logged), want) {
			t.Fatalf("client log misses %q; log = %q", want, logged)
		}
	}
}
