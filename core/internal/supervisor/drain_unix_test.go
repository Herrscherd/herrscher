//go:build !windows

package supervisor

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

	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	drainPIDWait       = 10 * time.Second
	fakeBridgeLifetime = BridgeTermGrace + 3*time.Second
)

func TestDrainBridgesReapsBridgeThatIgnoresSIGTERM(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "pid")
	script := filepath.Join(dir, "bridge.sh")
	body := fmt.Sprintf("#!/bin/sh\ntrap '' TERM\necho $$ > '%s'\nexec sleep %d\n", pidPath, int(fakeBridgeLifetime/time.Second))
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake bridge: %v", err)
	}

	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	sup := NewSupervisor(parent, script, testLayout(t))
	sup.Start(state.Session{Name: "drain", Dir: dir})

	pid := waitForBridgePID(t, pidPath)
	t.Cleanup(func() {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Errorf("kill leftover bridge %d: %v", pid, err)
		}
	})

	cancelParent()
	if err := sup.DrainBridges(); err != nil {
		t.Fatalf("DrainBridges returned %v, want every bridge loop to have returned", err)
	}
	if bridgeAlive(pid) {
		t.Fatalf("bridge %d outlived the supervisor drain", pid)
	}
}

func waitForBridgePID(t *testing.T, pidPath string) int {
	t.Helper()
	deadline := time.Now().Add(drainPIDWait)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(pidPath)
		if err == nil && strings.TrimSpace(string(raw)) != "" {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if convErr != nil {
				t.Fatalf("pid file holds %q: %v", raw, convErr)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fake bridge never wrote its pid")
	return 0
}

func bridgeAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || !errors.Is(err, syscall.ESRCH)
}
