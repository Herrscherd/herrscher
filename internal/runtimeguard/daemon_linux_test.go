//go:build linux

package runtimeguard

import (
	"bufio"
	"context"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

const (
	daemonSocketVar  = "RUNTIMEGUARD_TEST_DAEMON_SOCKET"
	daemonLockVar    = "RUNTIMEGUARD_TEST_DAEMON_LOCK"
	daemonSocketName = "herrscher-command-daemon.sock"
	daemonLockName   = "herrscher-instance-daemon.lock"
	helperReady      = "ready\n"
)

func TestRuntimeguardTestDaemon(t *testing.T) {
	socket := os.Getenv(daemonSocketVar)
	if socket == "" {
		t.Skip("helper process for the daemon tests, started by them")
	}
	if lock := os.Getenv(daemonLockVar); lock != "" {
		lockHeldByHelper(t, lock)
	}
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	if _, writeErr := os.Stdout.WriteString(helperReady); writeErr != nil {
		t.Error(writeErr)
	}
	if _, copyErr := io.Copy(io.Discard, os.Stdin); copyErr != nil {
		t.Error(copyErr)
	}
	if closeErr := listener.Close(); closeErr != nil {
		t.Error(closeErr)
	}
}

func lockHeldByHelper(t *testing.T, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if lockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); lockErr != nil {
		t.Fatal(lockErr)
	}
}

func TestTheChurnOfADaemonAloneDoesNotFailTheGuard(t *testing.T) {
	dir := shortRuntimeDir(t)
	guard := Start(dir)
	startDaemon(t, dir)
	if changes := changesOf(t, guard); len(changes) != 0 {
		t.Fatalf("changes = %q, want none: the daemon holds its socket and its instance lock", changes)
	}
}

func TestATestLeakWhileADaemonIsLiveFailsTheGuard(t *testing.T) {
	dir := shortRuntimeDir(t)
	startDaemon(t, dir)
	guard := Start(dir)
	leakSocket(t, filepath.Join(dir, "herrscher-command-outside.sock"))
	if changes := changesOf(t, guard); !containsChange(changes, "herrscher-command-outside.sock: added") {
		t.Fatalf("changes = %q, want the leaked socket reported while a daemon is live", changes)
	}
}

func TestADeletionOfAPreExistingDaemonSocketIsFlagged(t *testing.T) {
	dir := shortRuntimeDir(t)
	startDaemon(t, dir)
	guard := Start(dir)
	if err := os.Remove(filepath.Join(dir, daemonSocketName)); err != nil {
		t.Fatal(err)
	}
	if changes := changesOf(t, guard); !containsChange(changes, daemonSocketName+": removed") {
		t.Fatalf("changes = %q, want the removal of a pre-existing socket reported even though a daemon holds it", changes)
	}
}

func TestAnInstanceLockNobodyHoldsIsFlagged(t *testing.T) {
	dir := shortRuntimeDir(t)
	guard := Start(dir)
	if err := writeFile(filepath.Join(dir, "herrscher-instance-leak.lock"), "", oldTime()); err != nil {
		t.Fatal(err)
	}
	if changes := changesOf(t, guard); !containsChange(changes, "herrscher-instance-leak.lock: added") {
		t.Fatalf("changes = %q, want an unheld instance lock reported", changes)
	}
}

func TestAChildOfTheTestHoldingASocketIsFlagged(t *testing.T) {
	dir := shortRuntimeDir(t)
	guard := Start(dir)
	startChild(t, filepath.Join(dir, "herrscher-command-child.sock"))
	if changes := changesOf(t, guard); !containsChange(changes, "herrscher-command-child.sock: added") {
		t.Fatalf("changes = %q, want a socket held by a child of the test reported", changes)
	}
}

func TestASocketIsFlaggedWhenAttributionIsUnavailable(t *testing.T) {
	dir := shortRuntimeDir(t)
	guard := start(dir, unattributed)
	leakSocket(t, filepath.Join(dir, "herrscher-command-outside.sock"))
	changes := changesOf(t, guard)
	if !containsChange(changes, "herrscher-command-outside.sock: added") {
		t.Fatalf("changes = %q, want the socket reported without holder attribution", changes)
	}
	if !containsChange(changes, "run the tests without a herrscher daemon") {
		t.Fatalf("changes = %q, want the hint to run the tests without a daemon", changes)
	}
}

func shortSocketRoot() (string, error) {
	return os.MkdirTemp("/tmp", "rg")
}

func shortRuntimeDir(t *testing.T) string {
	t.Helper()
	dir, err := shortSocketRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			t.Error(removeErr)
		}
	})
	return dir
}

func startDaemon(t *testing.T, dir string) {
	t.Helper()
	stopReader, stopWriter := newPipe(t)
	readyReader, readyWriter := newPipe(t)
	cmd := helperCommand(t, filepath.Join(dir, daemonSocketName), filepath.Join(dir, daemonLockName),
		"sh", "-c", `"$0" -test.run=^TestRuntimeguardTestDaemon$ 0<&3 1>&4 &`, os.Args[0])
	cmd.ExtraFiles = []*os.File{stopReader, readyWriter}
	ready := bufio.NewReader(readyReader)
	t.Cleanup(func() {
		stopHelper(t, stopWriter, ready)
		closeFiles(t, readyReader)
	})
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	closeFiles(t, stopReader, readyWriter)
	awaitReady(t, ready)
}

func startChild(t *testing.T, socket string) {
	t.Helper()
	stopReader, stopWriter := newPipe(t)
	readyReader, readyWriter := newPipe(t)
	cmd := helperCommand(t, socket, "", os.Args[0], "-test.run=^TestRuntimeguardTestDaemon$")
	cmd.Stdin = stopReader
	cmd.Stdout = readyWriter
	ready := bufio.NewReader(readyReader)
	t.Cleanup(func() {
		stopHelper(t, stopWriter, ready)
		closeFiles(t, readyReader)
		if waitErr := cmd.Wait(); waitErr != nil {
			t.Error(waitErr)
		}
	})
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	closeFiles(t, stopReader, readyWriter)
	awaitReady(t, ready)
}

func helperCommand(t *testing.T, socket, lock, name string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(context.WithoutCancel(t.Context()), name, args...)
	cmd.Env = append(os.Environ(), daemonSocketVar+"="+socket)
	if lock != "" {
		cmd.Env = append(cmd.Env, daemonLockVar+"="+lock)
	}
	return cmd
}

func newPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	return reader, writer
}

func closeFiles(t *testing.T, files ...*os.File) {
	t.Helper()
	for _, file := range files {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}
}

func stopHelper(t *testing.T, stop *os.File, output *bufio.Reader) {
	t.Helper()
	closeFiles(t, stop)
	if _, err := io.Copy(io.Discard, output); err != nil {
		t.Error(err)
	}
}

func awaitReady(t *testing.T, output *bufio.Reader) {
	t.Helper()
	line, err := output.ReadString('\n')
	if err != nil || line != helperReady {
		t.Fatalf("the test helper did not come up: %q, %v", line, err)
	}
}

func leakSocket(t *testing.T, path string) {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	unixListener, ok := listener.(*net.UnixListener)
	if !ok {
		t.Fatalf("listener is %T, want *net.UnixListener", listener)
	}
	unixListener.SetUnlinkOnClose(false)
	if closeErr := unixListener.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
}

func unattributed(string, map[string]fs.FileInfo) (owners, error) {
	return owners{}, nil
}
