//go:build unix

package main

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const shortTempRoot = "/tmp"

func isolatedRuntimeRoot(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(shortTempRoot, "rt"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func serveChild(t *testing.T) (*exec.Cmd, *bufio.Scanner) {
	t.Helper()
	runtimeRoot := isolatedRuntimeRoot(t)
	dir := t.TempDir()
	envFile := filepath.Join(dir, "herrscher.env")
	if err := os.WriteFile(envFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	logs, logsWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := logs.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	child := exec.CommandContext(t.Context(), os.Args[0])
	child.Dir = dir
	child.Stderr = logsWriter
	child.Env = []string{
		reexecMarker + "=serve",
		"HERRSCHER_ENV_FILE=" + envFile,
		"HOME=" + dir,
		"HERRSCHER_STATE_DIR=" + filepath.Join(dir, "state"),
		"XDG_RUNTIME_DIR=" + runtimeRoot,
		"PATH=" + os.Getenv("PATH"),
	}
	startErr := child.Start()
	if closeErr := logsWriter.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if startErr != nil {
		t.Fatal(startErr)
	}
	return child, bufio.NewScanner(logs)
}

func exitCodeAfter(t *testing.T, sig os.Signal) int {
	t.Helper()
	child, lines := serveChild(t)
	up := false
	for !up && lines.Scan() {
		up = strings.Contains(lines.Text(), "hub up")
	}
	if !up {
		t.Fatalf("serve never reported the hub up: %v", lines.Err())
	}
	if err := child.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	for lines.Scan() {
	}
	err := child.Wait()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return exit.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0
}

func TestServeStoppedByTerminateExitsCleanly(t *testing.T) {
	if code := exitCodeAfter(t, syscall.SIGTERM); code != 0 {
		t.Fatalf("serve stopped by SIGTERM exited %d, want 0", code)
	}
}

func TestServeStoppedByInterruptExitsAsInterrupted(t *testing.T) {
	if code := exitCodeAfter(t, os.Interrupt); code != exitInterrupted {
		t.Fatalf("serve stopped by an interrupt exited %d, want %d", code, exitInterrupted)
	}
}
