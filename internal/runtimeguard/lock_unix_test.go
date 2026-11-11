//go:build unix

package runtimeguard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func holdInstanceLock(t *testing.T, dir string) {
	t.Helper()
	descriptor, err := unix.Open(filepath.Join(dir, "herrscher-instance-test.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := unix.Close(descriptor); closeErr != nil {
			t.Errorf("close the instance lock: %v", closeErr)
		}
	})
	if err := unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatalf("take the instance lock: %v", err)
	}
}

func TestAnOutsideChangeWhileADaemonHoldsItsLockDoesNotFailTheGuard(t *testing.T) {
	dir := t.TempDir()
	holdInstanceLock(t, dir)
	guard := Start(dir)
	writeOutsideChanges(t, dir)
	if code := guard.Finish(0); code != 0 {
		t.Fatalf("Finish = %d, want 0: a live daemon's churn is not the tests' change", code)
	}
}

func TestADaemonThatStartsDuringTheRunDoesNotFailTheGuard(t *testing.T) {
	dir := t.TempDir()
	guard := Start(dir)
	holdInstanceLock(t, dir)
	writeOutsideChanges(t, dir)
	if code := guard.Finish(0); code != 0 {
		t.Fatalf("Finish = %d, want 0: a daemon started during the run owns its changes", code)
	}
}

func TestAChangeWithoutADaemonStillFailsTheGuard(t *testing.T) {
	dir := t.TempDir()
	guard := Start(dir)
	writeOutsideChanges(t, dir)
	if code := guard.Finish(0); code != 1 {
		t.Fatalf("Finish = %d, want 1: nothing holds the lock, so the change is the tests'", code)
	}
}

func writeOutsideChanges(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "herrscher-command-outside.sock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}
