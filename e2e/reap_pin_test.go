//go:build e2e && linux

package e2e_test

import (
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

func TestReapInstanceKillsNothingWhenTheSecondScanFails(t *testing.T) {
	instance := newTestInstance(t)
	root := t.TempDir()
	child := exec.CommandContext(t.Context(), "sh", "-c", `root=$1; trap 'f="$root/$$/environ"; rm -f "$f"; mkdir "$f"' INT; echo ready; while :; do sleep 0.05; done`, "sh", root)
	child.Env = []string{instanceEntry(instance)}
	startReady(t, child)
	killAtEnd(t, child)
	fakeProcess(t, root, child.Process.Pid, instanceEntry(instance)+"\x00")
	_, _, err := reapInstance(root, instance, 2*time.Second)
	if !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("got %v, want the second scan to fail on the replaced environment", err)
	}
	if !stillRunningAfter(child, signalSettle) {
		t.Fatal("the second scan failed, yet the process was stopped: it must keep running")
	}
}

func TestProcessesInMatchesTheWholeEntry(t *testing.T) {
	instance := newTestInstance(t)
	entry := instanceEntry(instance)
	root := t.TempDir()
	fakeProcess(t, root, 101, "NOT"+entry+"\x00")
	fakeProcess(t, root, 102, entry+"-other\x00")
	fakeProcess(t, root, 103, "PREFIX=x\x00"+entry+"\x00")
	pids, err := processesIn(root, entry)
	if err != nil {
		t.Fatal(err)
	}
	if len(pids) != 1 || pids[0] != 103 {
		t.Fatalf("got %v, want only the process whose environment holds the exact entry", pids)
	}
}

func TestReapInstanceRefusesAMalformedInstanceWithoutSignalling(t *testing.T) {
	malformed := "e2e-xyz"
	carrier := exec.CommandContext(t.Context(), "sleep", "60")
	carrier.Env = []string{instanceEntry(malformed)}
	if err := carrier.Start(); err != nil {
		t.Fatal(err)
	}
	killAtEnd(t, carrier)
	if _, _, err := reapInstance(procRoot, malformed, reapGrace); err == nil {
		t.Fatal("a malformed instance id was accepted")
	}
	if !stillRunning(carrier) {
		t.Fatal("a process was signalled for a malformed instance id")
	}
}

func TestReapInstanceCountsNothingForAProcessThatEndedBeforeTheInterrupt(t *testing.T) {
	instance := newTestInstance(t)
	root := t.TempDir()
	running := exec.CommandContext(t.Context(), "sleep", "60")
	if err := running.Start(); err != nil {
		t.Fatal(err)
	}
	killAtEnd(t, running)
	fakeProcess(t, root, running.Process.Pid, instanceEntry(instance)+"\x00")
	endedAlready := func(candidate int) (int, error) {
		fakeStatWithState(t, root, candidate, "Z", fakeStart)
		return openPidfd(candidate)
	}
	stopped, survivors, err := reapInstanceWith(root, instance, reapGrace, endedAlready)
	if err != nil || len(stopped) != 0 || len(survivors) != 0 {
		t.Fatalf("got stopped %q, survivors %q, %v: a process that had already ended before the interrupt is neither stopped nor killed, so it counts for nothing", stopped, survivors, err)
	}
	if !stillRunningAfter(running, signalSettle) {
		t.Fatal("a process that had already ended was signalled, so it was not left alone")
	}
}

func TestReapInstanceCountsAProcessThatEndsAfterTheInterruptAsStopped(t *testing.T) {
	instance := newTestInstance(t)
	child := exec.CommandContext(t.Context(), "sh", "-c", "trap 'exit 0' INT; echo ready; while :; do sleep 0.05; done")
	child.Env = []string{instanceEntry(instance)}
	startReady(t, child)
	t.Cleanup(func() { _ = child.Process.Kill() })
	stopped, survivors, err := reapInstance(procRoot, instance, reapGrace)
	if err != nil || len(survivors) != 0 || len(stopped) == 0 {
		t.Fatalf("got stopped %q, survivors %q, %v: a process that received the interrupt and ended within the grace counts as stopped, and is never killed", stopped, survivors, err)
	}
	if waitErr := child.Wait(); waitErr != nil {
		t.Fatalf("the process ended with %v, want its own exit with status 0 after the interrupt, not a signal", waitErr)
	}
}

func killAtEnd(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	t.Cleanup(func() {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Error(err)
		}
		if _, err := cmd.Process.Wait(); err != nil {
			t.Error(err)
		}
	})
}

func newTestInstance(t *testing.T) string {
	t.Helper()
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	return instance
}

const fakeStart = "1000"

func fakeProcess(t *testing.T, root string, pid int, environ string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "environ"), []byte(environ), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeStat(t, root, pid, fakeStart)
}

func fakeStat(t *testing.T, root string, pid int, start string) {
	t.Helper()
	fakeStatWithState(t, root, pid, "S", start)
}

func fakeStatWithState(t *testing.T, root string, pid int, state, start string) {
	t.Helper()
	line := strconv.Itoa(pid) + " (fake) " + state + strings.Repeat(" 0", 18) + " " + start + "\n"
	if err := os.WriteFile(filepath.Join(root, strconv.Itoa(pid), "stat"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

const signalSettle = 300 * time.Millisecond

func stillRunningAfter(cmd *exec.Cmd, window time.Duration) bool {
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		if !stillRunning(cmd) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return stillRunning(cmd)
}

func stillRunning(cmd *exec.Cmd) bool {
	wpid, err := syscall.Wait4(cmd.Process.Pid, nil, syscall.WNOHANG, nil)
	return err == nil && wpid == 0
}
