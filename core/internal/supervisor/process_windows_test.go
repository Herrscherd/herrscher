//go:build windows

package supervisor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const (
	processHelperRoleEnv  = "HERRSCHER_SUPERVISOR_PROCESS_HELPER"
	processHelperLockEnv  = "HERRSCHER_SUPERVISOR_PROCESS_LOCK"
	processHelperReadyEnv = "HERRSCHER_SUPERVISOR_PROCESS_READY"
)

func TestMain(m *testing.M) {
	switch os.Getenv(processHelperRoleEnv) {
	case "root":
		os.Exit(runProcessTreeRootHelper())
	case "child":
		os.Exit(runProcessTreeChildHelper())
	default:
		os.Exit(m.Run())
	}
}

func runProcessTreeRootHelper() int {
	exe, err := os.Executable()
	if err != nil {
		return 2
	}
	cmd := exec.Command(exe)
	cmd.Dir = os.Getenv(processHelperLockEnv)
	cmd.Env = helperEnvironment("child")
	if err := cmd.Start(); err != nil {
		return 3
	}
	if err := cmd.Wait(); err != nil {
		return 4
	}
	return 0
}

func runProcessTreeChildHelper() int {
	lockPath, err := windows.UTF16PtrFromString(os.Getenv(processHelperLockEnv))
	if err != nil {
		return 5
	}
	handle, err := windows.CreateFile(
		lockPath,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return 6
	}
	defer windows.CloseHandle(handle)
	if err := os.WriteFile(
		os.Getenv(processHelperReadyEnv),
		[]byte(strconv.Itoa(os.Getpid())),
		0o600,
	); err != nil {
		return 7
	}
	for {
		time.Sleep(time.Hour)
	}
}

func helperEnvironment(role string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !strings.EqualFold(key, processHelperRoleEnv) {
			env = append(env, value)
		}
	}
	return append(env, processHelperRoleEnv+"="+role)
}

func waitForHelperPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(string(raw))
			if parseErr != nil {
				t.Fatalf("parse helper pid %q: %v", raw, parseErr)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process-tree helper did not become ready")
	return 0
}

func killTestProcessTree(pid int) {
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		return
	}
	_ = exec.Command(
		filepath.Join(systemDir, "taskkill.exe"),
		"/PID", strconv.Itoa(pid), "/T", "/F",
	).Run()
}

func TestConfiguredBridgeCancellationTerminatesDescendantHoldingWorktree(t *testing.T) {
	base := t.TempDir()
	lockDir := filepath.Join(base, "locked-worktree")
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	readyPath := filepath.Join(base, "ready")
	t.Setenv(processHelperRoleEnv, "root")
	t.Setenv(processHelperLockEnv, lockDir)
	t.Setenv(processHelperReadyEnv, readyPath)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, os.Args[0])
	cmd.Dir = lockDir
	configureBridgeCommand(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	childPID := waitForHelperPID(t, readyPath)
	defer func() {
		cancel()
		killTestProcessTree(cmd.Process.Pid)
		killTestProcessTree(childPID)
	}()

	if err := os.Remove(lockDir); err == nil {
		t.Fatal("helper did not lock its worktree directory")
	}

	cancel()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("bridge process did not exit after cancellation")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		err := os.Remove(lockDir)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendant still holds worktree after bridge cancellation: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
