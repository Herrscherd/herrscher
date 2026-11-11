//go:build linux

package runtimeguard

import (
	"bufio"
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const daemonSocketVar = "RUNTIMEGUARD_TEST_DAEMON_SOCKET"

func TestRuntimeguardTestDaemon(t *testing.T) {
	socket := os.Getenv(daemonSocketVar)
	if socket == "" {
		t.Skip("helper process for the daemon tests, started by them")
	}
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	if _, writeErr := os.Stdout.WriteString("ready\n"); writeErr != nil {
		t.Error(writeErr)
	}
	if _, copyErr := io.Copy(io.Discard, os.Stdin); copyErr != nil {
		t.Error(copyErr)
	}
	if closeErr := listener.Close(); closeErr != nil {
		t.Error(closeErr)
	}
}

func TestTheChurnOfADaemonAloneDoesNotFailTheGuard(t *testing.T) {
	dir := shortRuntimeDir(t)
	guard := Start(dir)
	startDaemon(t, filepath.Join(dir, "herrscher-command-daemon.sock"))
	if changes := changesOf(t, guard); len(changes) != 0 {
		t.Fatalf("changes = %q, want none: the daemon owns the socket it holds open", changes)
	}
}

func TestATestLeakWhileADaemonIsLiveFailsTheGuard(t *testing.T) {
	dir := shortRuntimeDir(t)
	startDaemon(t, filepath.Join(dir, "herrscher-command-daemon.sock"))
	guard := Start(dir)
	leakSocket(t, filepath.Join(dir, "herrscher-command-outside.sock"))
	if changes := changesOf(t, guard); !containsChange(changes, "herrscher-command-outside.sock: added") {
		t.Fatalf("changes = %q, want the leaked socket reported while a daemon is live", changes)
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

func startDaemon(t *testing.T, socket string) {
	t.Helper()
	cmd := exec.CommandContext(context.WithoutCancel(t.Context()), os.Args[0], "-test.run=^TestRuntimeguardTestDaemon$")
	cmd.Env = append(os.Environ(), daemonSocketVar+"="+socket)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if startErr := cmd.Start(); startErr != nil {
		t.Fatal(startErr)
	}
	t.Cleanup(func() {
		if closeErr := stdin.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		if waitErr := cmd.Wait(); waitErr != nil {
			t.Error(waitErr)
		}
	})
	ready, readErr := bufio.NewReader(stdout).ReadString('\n')
	if readErr != nil || ready != "ready\n" {
		t.Fatalf("the test daemon did not come up: %q, %v", ready, readErr)
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
