package main

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProcessAliveCountsAZombieAsDead(t *testing.T) {
	child := exec.CommandContext(t.Context(), "true")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	pid := child.Process.Pid
	waitForZombie(t, pid)
	if processAlive(pid) {
		t.Errorf("processAlive(%d) is true for an exited child that nobody has reaped", pid)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessAliveCountsARunningProcessAsAlive(t *testing.T) {
	child := exec.CommandContext(t.Context(), "sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	pid := child.Process.Pid
	alive := processAlive(pid)
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	var exitErr *exec.ExitError
	if err := child.Wait(); !errors.As(err, &exitErr) {
		t.Fatalf("wait for the killed sleep: %v", err)
	}
	if !alive {
		t.Errorf("processAlive(%d) is false for a running process", pid)
	}
}

func waitForZombie(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(groupWaitBound)
	for {
		raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err == nil && strings.Contains(string(raw), ") Z ") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d never became a zombie", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
