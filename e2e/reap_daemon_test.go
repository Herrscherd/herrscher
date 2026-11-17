//go:build e2e && linux

package e2e_test

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

const readyTimeout = 10 * time.Second

func startReady(t *testing.T, cmd *exec.Cmd) *exec.Cmd {
	t.Helper()
	ready, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := ready.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	cmd.Stdout = write
	startErr := cmd.Start()
	if closeErr := write.Close(); closeErr != nil {
		t.Error(closeErr)
	}
	if startErr != nil {
		t.Fatal(startErr)
	}
	awaitReady(t, ready)
	return cmd
}

func awaitReady(tb testing.TB, ready *os.File) {
	tb.Helper()
	if err := ready.SetReadDeadline(time.Now().Add(readyTimeout)); err != nil {
		tb.Fatal(err)
	}
	line, err := bufio.NewReader(ready).ReadString('\n')
	if errors.Is(err, os.ErrDeadlineExceeded) {
		tb.Fatalf("no ready line within %s, so the process did not install its interrupt trap", readyTimeout)
	}
	if err != nil || line != "ready\n" {
		tb.Fatalf("the process did not install its interrupt trap: got %q, %v", line, err)
	}
}

func stopDaemon(tb testing.TB, daemon *exec.Cmd) {
	tb.Helper()
	if daemon.Process == nil || daemon.ProcessState != nil {
		return
	}
	if err := syscall.Kill(-daemon.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		tb.Error(err)
	}
	var exitErr *exec.ExitError
	if err := daemon.Wait(); err != nil && !errors.As(err, &exitErr) {
		tb.Error(err)
	}
}
