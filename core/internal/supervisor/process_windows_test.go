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
	cmd := exec.CommandContext(context.Background(), exe)
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
	_, err = windows.CreateFile(
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
	type helperLine struct {
		text string
		err  error
	}
	line := make(chan helperLine, 1)
	go func() {
		text, err := bufio.NewReader(ready).ReadString('\n')
		line <- helperLine{text: text, err: err}
	}()
	select {
	case got := <-line:
		if got.err != nil {
			t.Fatalf("read helper pid: %v", got.err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(got.text))
		if err != nil {
			t.Fatalf("parse helper pid %q: %v", got.text, err)
		}
		return pid
	case <-time.After(5 * time.Second):
		t.Fatalf("process-tree helper did not become ready")
	}
	return 0
}

func killTestProcessTree(tb testing.TB, pid int) {
	tb.Helper()
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		tb.Logf("locate system directory: %v", err)
		return
	}
	killer := exec.CommandContext(
		context.Background(),
		filepath.Join(systemDir, "taskkill.exe"),
		"/PID", strconv.Itoa(pid), "/T", "/F",
	)
	if err := killer.Run(); err != nil {
		tb.Logf("taskkill process tree %d: %v", pid, err)
	}
}

func closeTestResource(tb testing.TB, closer io.Closer) {
	tb.Helper()
	if err := closer.Close(); err != nil {
		tb.Errorf("close %T: %v", closer, err)
	}
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
	defer closeTestResource(t, readyReader)

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, exe)
	cmd.Dir = lockDir
	cmd.Stdout = readyWriter
	configureBridgeCommand(ctx, cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, readyWriter)
	childPID := waitForHelperPID(t, readyReader)
	defer func() {
		cancel()
		killTestProcessTree(t, cmd.Process.Pid)
		killTestProcessTree(t, childPID)
	}()

	if err := os.Remove(lockDir); err == nil {
		t.Fatal("helper did not lock its worktree directory")
	}

	cancel()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case waitErr := <-waited:
		t.Logf("bridge process exited after cancellation: %v", waitErr)
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
	event, waitErr := windows.WaitForSingleObject(handle, uint32(timeout/time.Millisecond))
	closeErr := windows.CloseHandle(handle)
	if err := errors.Join(waitErr, closeErr); err != nil {
		return err
	}
	if event != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("process %d still running after %v", pid, timeout)
	}
	return nil
}
