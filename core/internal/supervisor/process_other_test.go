//go:build !windows

package supervisor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	bridgeHelperRoleEnv   = "HERRSCHER_SUPERVISOR_BRIDGE_HELPER"
	bridgeHelperMarkerEnv = "HERRSCHER_SUPERVISOR_BRIDGE_MARKER"
	bridgeHelperTrap      = "trap"
	bridgeHelperIgnore    = "ignore"
	bridgeHelperReady     = "ready"
	bridgeHelperTimeout   = 15 * time.Second
)

func TestBridgeSignalHelper(t *testing.T) {
	role := os.Getenv(bridgeHelperRoleEnv)
	if role == "" {
		t.Skip("local bridge signal helper runs only when spawned by a test")
	}
	switch role {
	case bridgeHelperTrap:
		runTrapHelper(t, os.Getenv(bridgeHelperMarkerEnv))
	case bridgeHelperIgnore:
		runIgnoreHelper(t)
	default:
		t.Fatalf("unknown helper role %q", role)
	}
}

func runTrapHelper(t *testing.T, markerPath string) {
	t.Helper()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	announceReady(t)
	select {
	case <-signals:
	case <-time.After(bridgeHelperTimeout):
		t.Fatal("helper never received SIGTERM")
	}
	if err := os.WriteFile(markerPath, []byte(syscall.SIGTERM.String()), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
}

func runIgnoreHelper(t *testing.T) {
	t.Helper()
	signal.Ignore(syscall.SIGTERM)
	announceReady(t)
	time.Sleep(bridgeHelperTimeout)
}

func announceReady(t *testing.T) {
	t.Helper()
	if _, err := fmt.Fprintln(os.Stdout, bridgeHelperReady); err != nil {
		t.Fatalf("announce readiness: %v", err)
	}
}

func runBridgeHelperUntilCancel(t *testing.T, role, markerPath string) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBridgeSignalHelper$")
	cmd.Env = append(os.Environ(), bridgeHelperRoleEnv+"="+role, bridgeHelperMarkerEnv+"="+markerPath)
	configureBridgeCommand(ctx, cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Errorf("kill leftover helper: %v", err)
		}
	})

	announced := make(chan error, 1)
	go func() {
		line, readErr := bufio.NewReader(stdout).ReadString('\n')
		if readErr == nil && strings.TrimSpace(line) != bridgeHelperReady {
			readErr = fmt.Errorf("helper announced %q, want %q", line, bridgeHelperReady)
		}
		announced <- readErr
	}()
	select {
	case err := <-announced:
		if err != nil {
			t.Fatalf("helper not ready: %v", err)
		}
	case <-time.After(bridgeHelperTimeout):
		t.Fatal("helper did not announce readiness in time")
	}

	cancel()

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		return err
	case <-time.After(bridgeHelperTimeout):
		t.Fatal("helper did not exit after context cancellation")
		return nil
	}
}

func TestBridgeCancelDeliversSIGTERM(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "sigterm")

	if err := runBridgeHelperUntilCancel(t, bridgeHelperTrap, marker); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("helper should exit after handling SIGTERM, got %v", err)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("helper did not receive SIGTERM: %v", err)
	}
	if string(got) != syscall.SIGTERM.String() {
		t.Fatalf("marker = %q, want %q", got, syscall.SIGTERM.String())
	}
}

func TestBridgeCancelKillsIgnoringHelperAfterGrace(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "unused")

	started := time.Now()
	err := runBridgeHelperUntilCancel(t, bridgeHelperIgnore, marker)
	elapsed := time.Since(started)
	if elapsed < BridgeTermGrace {
		t.Fatalf("helper was killed after %v, before the %v grace delay", elapsed, BridgeTermGrace)
	}
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		t.Fatalf("expected the helper to be killed, got %v", err)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("helper ended with %v, want SIGKILL after the grace delay", exitErr)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("ignoring helper should not write a marker, stat err = %v", statErr)
	}
}
