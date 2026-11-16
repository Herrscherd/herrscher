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
	if !stillRunning(child) {
		t.Fatal("the process was ended although the second scan failed: nothing may be signalled on an unverified list")
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
	line := strconv.Itoa(pid) + " (fake) S" + strings.Repeat(" 0", 18) + " " + start + "\n"
	if err := os.WriteFile(filepath.Join(root, strconv.Itoa(pid), "stat"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

func stillRunning(cmd *exec.Cmd) bool {
	wpid, err := syscall.Wait4(cmd.Process.Pid, nil, syscall.WNOHANG, nil)
	return err == nil && wpid == 0
}
