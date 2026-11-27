//go:build unix

package mcpweight

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestProcessGoneHelperCountsAZombieAsGone(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("process states are read from /proc")
	}
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		if err := cmd.Wait(); err != nil {
			t.Errorf("reap zombie %d: %v", pid, err)
		}
	})
	awaitZombie(t, pid)
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("a zombie must still answer kill(pid, 0), got %v", err)
	}
	if !processGone(pid) {
		t.Fatalf("zombie %d is reported alive, want gone", pid)
	}
}

func awaitZombie(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err := processState(pid)
		if err == nil && state == "Z" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d did not become a zombie (state %q, err %v)", pid, state, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStopReleasesTheOutputOfAServerWhoseGrandchildKeepsIt(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "grandchild.pid")
	ctx, cancel := context.WithTimeout(t.Context(), DefaultTimeout)
	defer cancel()
	session, err := start(ctx, fakeServerWithEnv("escaping", map[string]string{fakePIDFileEnv: pidPath}), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := listTools(ctx, session); err != nil {
		t.Fatalf("listTools() error = %v, want the tools", err)
	}
	killOnCleanup(t, readPID(t, pidPath))
	if err := session.stop(); !errors.Is(err, errOutputOpen) {
		t.Fatalf("stop() error = %v, want the output still open", err)
	}
	select {
	case <-session.readerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the output reader is still running after stop released the output")
	}
}
