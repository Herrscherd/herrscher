//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

func TestClientSessionKeepsStderrQuiet(t *testing.T) {
	dir := t.TempDir()
	terminalCapture := filepath.Join(dir, "terminal.txt")
	clientLog := filepath.Join(dir, "client.log")

	restoreTerminal, err := redirectStderrCapped(terminalCapture, maxLogFileBytes)
	if err != nil {
		t.Fatalf("redirect terminal: %v", err)
	}
	sessionErr := withClientLog(silentLogger(), clientLog, func() error {
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

var errTUIExited = errors.New("tui exited")

func captureTerminal(t *testing.T, run func() error) (string, error) {
	t.Helper()
	capture := filepath.Join(t.TempDir(), "terminal.txt")
	restore, err := redirectStderrCapped(capture, maxLogFileBytes)
	if err != nil {
		t.Fatalf("redirect terminal: %v", err)
	}
	runErr := run()
	if restoreErr := restore(); restoreErr != nil {
		t.Fatalf("restore terminal: %v", restoreErr)
	}
	onTerminal, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("read terminal capture: %v", err)
	}
	return string(onTerminal), runErr
}

func writeFreshLine() error {
	fmt.Fprintln(os.Stderr, "fresh line")
	return nil
}

func TestClientLogOpenFailureRunsTheTUIOnTheTerminal(t *testing.T) {
	var out bytes.Buffer
	missing := filepath.Join(t.TempDir(), "absent", "client.log")
	ran := false
	onTerminal, err := captureTerminal(t, func() error {
		return withClientLog(bufferedLogger(&out), missing, func() error {
			ran = true
			fmt.Fprintln(os.Stderr, "tui line on terminal")
			return errTUIExited
		})
	})
	if !ran || !errors.Is(err, errTUIExited) {
		t.Fatalf("with an unopenable client log: ran=%v err=%v, want the TUI to run and its error to come back", ran, err)
	}
	if !strings.Contains(out.String(), "client log unavailable") {
		t.Fatalf("no warning for the unopenable client log:\n%s", out.String())
	}
	if !strings.Contains(onTerminal, "tui line on terminal") {
		t.Fatalf("stderr must stay on the terminal when the client log cannot open, captured %q", onTerminal)
	}
}

func TestClientLogRotatesOverTheCapAndKeepsOneGeneration(t *testing.T) {
	clientLog := filepath.Join(t.TempDir(), "client.log")
	previous := clientLog + ".1"
	if err := os.WriteFile(clientLog, bytes.Repeat([]byte("x"), maxLogFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(previous, []byte("older generation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureTerminal(t, func() error {
		return withClientLog(silentLogger(), clientLog, writeFreshLine)
	}); err != nil {
		t.Fatal(err)
	}
	rotated, err := os.Stat(previous)
	if err != nil || rotated.Size() != maxLogFileBytes+1 {
		t.Fatalf("client.log.1 = %v, %v; want the oversized previous log moved there", rotated, err)
	}
	current, err := os.ReadFile(clientLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "fresh line") || strings.Contains(string(current), "x") {
		t.Fatalf("client.log after rotation = %d bytes, want only the new session", len(current))
	}
}

func TestClientLogAppendsBelowTheCap(t *testing.T) {
	clientLog := filepath.Join(t.TempDir(), "client.log")
	if err := os.WriteFile(clientLog, []byte("earlier line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureTerminal(t, func() error {
		return withClientLog(silentLogger(), clientLog, writeFreshLine)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(clientLog + ".1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a log under the cap must not rotate, stat .1 = %v", err)
	}
	current, err := os.ReadFile(clientLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "earlier line") || !strings.Contains(string(current), "fresh line") {
		t.Fatalf("client.log = %q, want the earlier and the new lines", current)
	}
}

func TestRunAttachedRoutesTheTUIThroughTheClientLog(t *testing.T) {
	clientLog := filepath.Join(t.TempDir(), "client.log")
	runner := attachRunner{
		log:     silentLogger(),
		logPath: clientLog,
		connect: func(ctx context.Context, _ string, _ i18n.Language) (*attachedDaemon, error) {
			return &attachedDaemon{done: ctx.Done(), events: make(chan tui.RoutedEvent)}, nil
		},
		run: func(_ context.Context, _ context.CancelFunc, _ tui.Backend, _ ...tui.Option) error {
			fmt.Fprintln(os.Stderr, "tui stderr line")
			return nil
		},
	}
	onTerminal, err := captureTerminal(t, func() error {
		return runner.runAttached(context.Background(), "instance")
	})
	if err != nil {
		t.Fatalf("runAttached: %v", err)
	}
	if onTerminal != "" {
		t.Fatalf("stderr reached the terminal during the attached TUI: %q", onTerminal)
	}
	logged, err := os.ReadFile(clientLog)
	if err != nil {
		t.Fatalf("read client log: %v", err)
	}
	if !strings.Contains(string(logged), "tui stderr line") {
		t.Fatalf("runAttached bypassed the client log; log = %q", logged)
	}
}
