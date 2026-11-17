//go:build e2e && linux

package e2e_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPinRefusesAProcessWhoseStartTimeChanged(t *testing.T) {
	instance := newTestInstance(t)
	entry := instanceEntry(instance)
	child := startSleeper(t)
	root := t.TempDir()
	fakeProcess(t, root, child.Process.Pid, entry+"\x00")
	fakeStat(t, root, child.Process.Pid, "2000")
	_, pinnedOK, err := pin(root, openPidfd, processRef{pid: child.Process.Pid, start: fakeStart}, entry)
	if err != nil || pinnedOK {
		t.Fatalf("got pinned=%v err=%v, want a refusal without error when the start time changed", pinnedOK, err)
	}
	if !stillRunning(child) {
		t.Fatal("a process whose start time changed was signalled")
	}
}

func TestPinCountsAProcessWhoseStatVanishedAsStopped(t *testing.T) {
	instance := newTestInstance(t)
	entry := instanceEntry(instance)
	child := startSleeper(t)
	root := t.TempDir()
	fakeProcess(t, root, child.Process.Pid, entry+"\x00")
	if err := os.Remove(filepath.Join(root, strconv.Itoa(child.Process.Pid), "stat")); err != nil {
		t.Fatal(err)
	}
	_, pinnedOK, err := pin(root, openPidfd, processRef{pid: child.Process.Pid, start: fakeStart}, entry)
	if err != nil || pinnedOK {
		t.Fatalf("got pinned=%v err=%v, want a process whose stat vanished counted as stopped", pinnedOK, err)
	}
	if !stillRunning(child) {
		t.Fatal("a process whose stat vanished was signalled")
	}
}

func TestScanIgnoresAnUnreadableEnvironmentWithoutSignalling(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads the environment of every process")
	}
	instance := newTestInstance(t)
	child := startSleeper(t)
	root := t.TempDir()
	fakeProcess(t, root, child.Process.Pid, instanceEntry(instance)+"\x00")
	environ := filepath.Join(root, strconv.Itoa(child.Process.Pid), "environ")
	if err := os.Chmod(environ, 0o000); err != nil {
		t.Fatal(err)
	}
	stopped, survivors, err := reapInstance(root, instance, reapGrace)
	if err != nil || len(stopped) != 0 || len(survivors) != 0 {
		t.Fatalf("got stopped=%v survivors=%v err=%v, want nothing reported for an unreadable environment", stopped, survivors, err)
	}
	if !stillRunning(child) {
		t.Fatal("a process with an unreadable environment was signalled")
	}
}

const pinnedSignalWait = 5 * time.Second

func TestSignalGoesThroughThePinnedDescriptor(t *testing.T) {
	pinnedChild := exec.CommandContext(t.Context(), "sleep", "60")
	if err := pinnedChild.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pinnedChild.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Error(err)
		}
	})
	other := startSleeper(t)
	fd, err := unix.PidfdOpen(pinnedChild.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	killErr := signalAll([]pinned{{pid: other.Process.Pid, fd: fd}}, syscall.SIGKILL)
	if err := errors.Join(killErr, unix.Close(fd)); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- pinnedChild.Wait() }()
	ended := false
	select {
	case err := <-exited:
		ended = err != nil
	case <-time.After(pinnedSignalWait):
	}
	if !ended {
		t.Fatal("the process behind the pinned descriptor was not signalled")
	}
	if !stillRunning(other) {
		t.Fatal("the signal went to the pid stored beside the descriptor instead of the pinned process")
	}
}

func TestReapInstanceSignalsNothingWithoutPidfd(t *testing.T) {
	instance := newTestInstance(t)
	child := startSleeper(t)
	root := t.TempDir()
	fakeProcess(t, root, child.Process.Pid, instanceEntry(instance)+"\x00")
	noPidfd := func(int) (int, error) { return -1, syscall.ENOSYS }
	if _, _, err := reapInstanceWith(root, instance, reapGrace, noPidfd); !errors.Is(err, syscall.ENOSYS) {
		t.Fatalf("got %v, want the refusal to pin reported as ENOSYS", err)
	}
	if !stillRunningAfter(child, signalSettle) {
		t.Fatal("a process was signalled without a pinned descriptor")
	}
}

func startSleeper(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	killAtEnd(t, cmd)
	return cmd
}
