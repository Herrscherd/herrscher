//go:build windows

package supervisor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const (
	processHelperRoleEnv = "HERRSCHER_SUPERVISOR_PROCESS_HELPER"
	processHelperLockEnv = "HERRSCHER_SUPERVISOR_PROCESS_LOCK"
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
	cmd.Stdout = os.Stdout
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
	if _, err := fmt.Fprintln(os.Stdout, os.Getpid()); err != nil {
		return 7
	}
	stop, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	<-stop.Done()
	return 0
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

func waitForHelperPID(t *testing.T, ready io.Reader) int {
	t.Helper()
	line := make(chan string, 1)
	go func() {
		text, _ := bufio.NewReader(ready).ReadString('\n')
		line <- text
	}()
	select {
	case text := <-line:
		pid, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil {
			t.Fatalf("parse helper pid %q: %v", text, err)
		}
		return pid
	case <-time.After(5 * time.Second):
		t.Fatalf("process-tree helper did not become ready")
	}
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
	t.Setenv(processHelperRoleEnv, "root")
	t.Setenv(processHelperLockEnv, lockDir)

	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readyReader.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, os.Args[0])
	cmd.Dir = lockDir
	cmd.Stdout = readyWriter
	configureBridgeCommand(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	readyWriter.Close()
	childPID := waitForHelperPID(t, readyReader)
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

	if err := waitForProcessExit(childPID, 2*time.Second); err != nil {
		t.Fatalf("descendant still holds worktree after bridge cancellation: %v", err)
	}
	if err := os.Remove(lockDir); err != nil {
		t.Fatalf("descendant still holds worktree after bridge cancellation: %v", err)
	}
}

func waitForProcessExit(pid int, timeout time.Duration) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	event, err := windows.WaitForSingleObject(handle, uint32(timeout/time.Millisecond))
	if err != nil {
		return err
	}
	if event != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("process %d still running after %v", pid, timeout)
	}
	return nil
}
