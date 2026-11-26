//go:build unix

package mcpweight

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func spawnGrandchildFor(mode string) error {
	switch mode {
	case "forking", "forking-silent":
		return spawnGrandchild(false)
	case "escaping":
		return spawnGrandchild(true)
	}
	return nil
}

func spawnGrandchild(detach bool) error {
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestFakeServer$")
	cmd.Env = append(os.Environ(), fakeModeEnv+"="+fakeModeGrand)
	cmd.Stdout = os.Stdout
	if detach {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	pidPath := os.Getenv(fakePIDFileEnv)
	if pidPath == "" {
		return nil
	}
	return os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("grandchild pid not written: %v", err)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatalf("grandchild pid %q is not a number: %v", raw, err)
	}
	return pid
}

func awaitProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if processGone(pid) {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("grandchild %d is still alive after its server was stopped", pid)
		}
	}
}

func processGone(pid int) bool {
	state, err := processState(pid)
	if err != nil {
		return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
	}
	return state == "Z"
}

func processState(pid int) (string, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	stat := string(raw)
	closing := strings.LastIndexByte(stat, ')')
	if closing < 0 || closing+2 >= len(stat) {
		return "", fmt.Errorf("malformed stat of process %d", pid)
	}
	return stat[closing+2 : closing+3], nil
}

func killOnCleanup(t *testing.T, pid int) {
	t.Helper()
	t.Cleanup(func() {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Errorf("kill grandchild %d: %v", pid, err)
		}
	})
}
