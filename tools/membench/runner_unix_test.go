//go:build unix

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const groupWaitBound = 20 * time.Second

func TestRunReturnsTheAnswerAndKillsTheGrandchild(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	binary := writeGroupScript(t, dir, "#!/bin/sh\nsleep 30 &\necho $! > '"+pidFile+"'\necho answer\nexit 0\n")
	r := runner{binary: binary, model: defaultModel}
	answer, err := r.invoke(context.Background(), groupTestBox(t), step{agent: agentAlpha, prompt: "x"}, nil)
	if err != nil {
		t.Fatalf("invoke returned %v for a run that exited 0", err)
	}
	if strings.TrimSpace(answer) != "answer" {
		t.Errorf("invoke returned %q, want the answer", answer)
	}
	waitGroupDead(t, readGroupPID(t, pidFile))
}

func TestCancelKillsTheGroupHoldingTheOutputPipe(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	binary := writeGroupScript(t, dir, "#!/bin/sh\nsleep 30 &\necho $! > '"+pidFile+"'\nwait\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	box := groupTestBox(t)
	done := make(chan error, 1)
	go func() {
		r := runner{binary: binary, model: defaultModel}
		_, err := r.invoke(ctx, box, step{agent: agentAlpha, prompt: "x"}, nil)
		done <- err
	}()
	pid := readGroupPID(t, pidFile)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a canceled call returned no error")
		}
	case <-time.After(groupWaitBound):
		t.Fatal("invoke did not return after the context was canceled")
	}
	waitGroupDead(t, pid)
}

func TestCancelSendsSIGINTToTheLeader(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	pidFile := filepath.Join(dir, "pid")
	script := fmt.Sprintf("#!/bin/sh\ntrap 'echo got > %s; exit 0' INT\nsleep 30 &\necho $! > %s\nwait\n", marker, pidFile)
	binary := writeGroupScript(t, dir, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	box := groupTestBox(t)
	done := make(chan error, 1)
	go func() {
		r := runner{binary: binary, model: defaultModel}
		_, err := r.invoke(ctx, box, step{agent: agentAlpha, prompt: "x"}, nil)
		done <- err
	}()
	pid := readGroupPID(t, pidFile)
	cancel()
	select {
	case err := <-done:
		t.Logf("invoke returned %v after the leader handled SIGINT", err)
	case <-time.After(groupWaitBound):
		t.Fatal("invoke did not return after the context was canceled")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the leader did not receive SIGINT: %v", err)
	}
	waitGroupDead(t, pid)
}

func TestOutputBeyondTheCapIsAnError(t *testing.T) {
	binary := writeGroupScript(t, t.TempDir(), fmt.Sprintf("#!/bin/sh\nhead -c %d /dev/zero\n", maxOutputBytes+1))
	r := runner{binary: binary, model: defaultModel}
	_, err := r.invoke(context.Background(), groupTestBox(t), step{agent: agentAlpha, prompt: "x"}, nil)
	if !errors.Is(err, errOutputTooLong) {
		t.Errorf("invoke returned %v, want the output-cap error", err)
	}
}

func writeGroupScript(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "fake")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func groupTestBox(t *testing.T) sandbox {
	t.Helper()
	box, err := newSandbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func readGroupPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(groupWaitBound)
	for {
		if raw, err := os.ReadFile(path); err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil {
				t.Cleanup(func() {
					if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
						t.Errorf("kill grandchild %d: %v", pid, err)
					}
				})
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the script never wrote the pid of its grandchild")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitGroupDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(groupWaitBound)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("the grandchild %d is still alive after its group was reaped", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
